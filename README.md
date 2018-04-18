# Hooker
Hooker is a tool to automatically update git repositories using a `POST Webhook`.

The master branch (fetched from `refs/remotes/origin/master`) gets updated only.

## Supported Git Services
* GitLab
* Bitbucket Server

## Configuration
The configuration is a simple toml file (`config.toml` by default, can be overriden using the `--config=PATH` flag).

## Adding Hooks
Just put a symlink to the repository (parent folder where the `.git` folder lies in) into the hooks path.

Assuming you have a repository named `test`, you would create a symlink named `test` into your hooks folder (`config: HookPath`).
In the settings for the repository, you would then add a `POST Hook` to `http://host:port/test`.
