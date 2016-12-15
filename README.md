## github-assignee-notifiler

Notify message when pull requests has assigned.

### macOS / OS X

Notify message uses `terminal-notifiler`. You need to install:

```
$ brew install terminal-notifier
```

### Windows

Still a work in progress...

### Usage

Download binary from [release page](https://github.com/ysugimoto/github-assignee-notifier/releases) on your architecture, and set the executable path.

`github-assignee-notifiler` uses some settigns. so you need to initialize:

```
$ github-assignee-notifiler init
```

And created config at `$HOME/.github_assinee_notifiler/config`.

|      key         |  type      |          value             |
|:------------:    |:------:    |:----------------------:    |
| name             | string     | Your github name           |
| token            | string     | github access token        |
| polling          | int        | polling duration (sec)     |
| repositories     | array      | repositories to watch      |

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
```
