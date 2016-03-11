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

// Ref Interface to support different webhooks
type Ref interface {
	Ref() string
}

// BitbucketWebhook stripped down to the bare minimum
type BitbucketWebhook struct {
	RefChanges []struct {
		RefID string `json:"refId"`
	} `json:"refChanges"`
}

// Ref Returns the Ref of the Change
func (b BitbucketWebhook) Ref() string {
	for _, r := range b.RefChanges {
		if r.RefID == "refs/heads/master" {
			return "refs/heads/master"
		}
	}
	return "not master"
}

func unmarshalPayload(r io.Reader) (Ref, error) {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Try BitBucket first
	var b BitbucketWebhook
	if err = json.Unmarshal(data, &b); err == nil {
		return b, nil
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

	remHead, err := repo.AnnotatedCommitFromRef(remoteRef)
	if err != nil {
		http500(fmt.Sprintf("could not get commit from ref: %s", err))
		return
	}

	if err := repo.Merge([]*git.AnnotatedCommit{remHead}, nil, nil); err != nil {
		http500(fmt.Sprintf("could not merge origin/master into local repo: %s", err))
		return
	}

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
