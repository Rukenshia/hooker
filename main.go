package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/julienschmidt/httprouter"
	"gopkg.in/libgit2/git2go.v22"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	configFile = flag.String("config", "config.toml", "configuration file")
)

// Config for our lovely hooker
type Config struct {
	HookPath string
	Host     string
	Port     uint16
}

var c Config
var mutex = &sync.Mutex{}

// Ref Interface to support different webhooks
type Ref interface {
	Ref() string
}

// BitbucketServerWebhook stripped down to the bare minimum
type BitbucketServerWebhook struct {
	RefChanges []struct {
		RefID string `json:"refId"`
	} `json:"refChanges"`
}

// GitLabWebhook, also just the bare minimum
type GitLabWebhook struct {
	ref string `json:"ref"`
}

// Ref Returns the Ref of the Change
func (b BitbucketServerWebhook) Ref() string {
	for _, r := range b.RefChanges {
		if r.RefID == "refs/heads/master" {
			return "refs/heads/master"
		}
	}
	return "not master"
}

// Ref returns the Ref of the change
func (w GitLabWebhook) Ref() string {
	return w.ref
}

func unmarshalPayload(r io.Reader) (Ref, error) {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	services := []interface{}{&BitbucketServerWebhook{}, &GitLabWebhook{}}
	for _, s := range services {
		if err := json.Unmarshal(data, &s); err == nil {
			return s.(Ref), nil
		}
	}
	return nil, err
}

func handleWebhook(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	ref, err := unmarshalPayload(r.Body)

	http500 := func(msg string) {
		log.Println(msg)
		http.Error(w, "500", http.StatusInternalServerError)
	}

	if err != nil {
		http500(fmt.Sprintf("invalid payload: %s", err))
		return
	}

	if ref.Ref() != "refs/heads/master" {
		http500(fmt.Sprintf("ignoring changeset on '%s', not a change on master\n", r.URL.Path))
		return
	}

	log.Printf("updating repository '%s'\n", filepath.Join(c.HookPath, r.URL.Path))
	mutex.Lock()
	defer mutex.Unlock()

	f, err := os.Stat(filepath.Join(c.HookPath, r.URL.Path))
	if err != nil {
		log.Printf("invalid repository '%s': %s\n", filepath.Join(c.HookPath, r.URL.Path), err)
		http.Error(w, "404", http.StatusNotFound)
		return
	}

	if !f.IsDir() {
		log.Printf("not a directory: '%s'\n", filepath.Join(c.HookPath, r.URL.Path))
		http.Error(w, "404", http.StatusNotFound)
		return
	}

	rdir := filepath.Join(c.HookPath, r.URL.Path, ".git")

	f, err = os.Stat(rdir)
	if err != nil {
		log.Println("not a git repository")
		http.Error(w, "403", http.StatusForbidden)
		return
	}

	if !f.IsDir() {
		log.Println(".git a file, not a repository")
		http.Error(w, "403", http.StatusForbidden)
		return
	}

	repo, err := git.OpenRepositoryExtended(rdir)
	if err != nil {
		http500("could not open git repository")
		return
	}

	remote, err := repo.LookupRemote("origin")
	if err != nil {
		http500("could not lookup remote 'origin'")
		return
	}

	if err := remote.Fetch(nil, nil, ""); err != nil {
		http500(fmt.Sprintf("could not fetch 'origin': %s", err))
		return
	}

	remoteRef, err := repo.LookupReference("refs/remotes/origin/master")
	if err != nil {
		http500(fmt.Sprintf("could not lookup 'refs/remotes/origin/master': %s", err))
		return
	}
	remoteTarget := remoteRef.Target()

	remHead, err := repo.AnnotatedCommitFromRef(remoteRef)
	if err != nil {
		http500(fmt.Sprintf("could not get commit from ref: %s", err))
		return
	}

	if err := repo.Merge([]*git.AnnotatedCommit{remHead}, nil, nil); err != nil {
		http500(fmt.Sprintf("could not merge origin/master into local repo: %s", err))
		return
	}

	// Point local brancht at remote
	remCommit, err := repo.LookupCommit(remoteTarget)
	if err != nil {
		http500(fmt.Sprintf("could not lookup commit on remote: %s", err))
		return
	}

	remTree, err := remCommit.Tree()
	if err != nil {
		http500(fmt.Sprintf("could not lookup remote tree: %s", err))
		return
	}

	if err := repo.CheckoutTree(remTree, &git.CheckoutOpts{Strategy: git.CheckoutForce}); err != nil {
		http500(fmt.Sprintf("could not checkout remote tree: %s", err))
		return
	}

	head, err := repo.Head()
	if err != nil {
		http500(fmt.Sprintf("could not get head: %s", err))
		return
	}

	localBranch, err := repo.LookupReference("refs/heads/master")
	if err != nil {
		http500(fmt.Sprintf("could not lookup local master: %s", err))
		return
	}

	localBranch.SetTarget(remoteTarget, nil, "")
	head.SetTarget(remoteTarget, nil, "")

	repo.StateCleanup()

	log.Printf("repository '%s' updated.\n", filepath.Join(c.HookPath, r.URL.Path))
	w.WriteHeader(200)
	w.Write([]byte("ok"))
}

func main() {
	flag.Parse()

	// taken from ascii-code.com
	fmt.Println(strings.Replace(`                                    ,-="""=.
                                  .'        ;.
                                 (            ;.
                                  ;.            ;..
                                   ,'             .'
                                   ;.            '.
                                     ;-.           ;-.
                                        )             ;=-.
                                      .'              ;=-.
                                    .;               .;-.
                      _            (                \ ;-.
                   ,'   ;.          ;.        /;.    \
                  /        ;.         \      |   ;.   ;.
                ,'            ;.       )    /      \    \
               /     .';.        ;.    )    |       ;.   \
             ,'    .'    ;.         ;./     \         ;.  \
           ,'    .'        ;.                \          \  \
         ,'    .'            ;.               \          ;. \
       ,'   .'                 ;.              )          ) (__.
     ,'   (                      ;.            )          ;."""'
 _.-'    __)                       ;.         .
;""'""                               ;"""""""`, ";", "`", -1))
	fmt.Println("")
	fmt.Println("                  hooker - bitbucket webhook deployment")
	fmt.Println()

	log.Println("loading config file")
	if _, err := toml.DecodeFile(*configFile, &c); err != nil {
		log.Fatal(err)
	}

	r := httprouter.New()
	r.POST("/*rest", handleWebhook)

	http.Handle("/", r)
	log.Println("starting server on", fmt.Sprintf("%s:%d", c.Host, c.Port))
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%d", c.Host, c.Port), nil))
}
