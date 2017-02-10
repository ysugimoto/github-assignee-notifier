## github-assignee-notifier

Notify message when you have been assigned pull requests.

### macOS / OS X

Notify message uses `terminal-notifier`. You need to install:

```
$ brew install terminal-notifier
```

### Windows

Still a work in progress...

### Usage

Download binary from [release page](https://github.com/ysugimoto/github-assignee-notifier/releases) on your architecture, and set the executable path.

`github-assignee-notifiler` uses some settings. So you can initialize this command:

```
$ github-assignee-notifiler init
```

And created config at `$HOME/.github_assinee_notifiler/config`, open your editor and put these section value(s):

|      key         |  type      |          value                |
|:------------:    |:------:    |:----------------------:       |
| name             | string     | Your github name              |
| token            | string     | Github access token           |
| polling          | int        | Polling duration (sec)        |
| repeat           | uint       | Repeat notify duration (sec) |
| repositories     | array      | Repositories to watch         |

After, you can watch the PRs simply:

```
$ github-assinee-notifier
```

### Notice

On macOS, Notification popup doesn't work in `tmux` mode. Please run in the normal terminal.

### Development

Get this repository and install dependency:

```
$ go get github.com/ysugimoto/github-assignee-notifier
$ cd src/github.com/ysugimoto/github-assignee-notifiler
$ glide up
$ make
```
