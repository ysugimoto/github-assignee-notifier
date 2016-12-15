.PHONY: static windows darwin

darwin: static
	GOOS=darwin GOARC=x64 go build -o build/github-assignee-notification main.go static.go

windows: static
	GOOS=windows GOARC=x64 go build -o build/github-assignee-notification.exe main.go static.go

static:
	go-bindata -o static.go etc/

release: darwin windows
	cd build && tar cvfz github-assignee-notification-darwin-x64.tar.gz github-assignee-notification
	cd build && zip github-assignee-notification-windows-x64.zip github-assignee-notification.exe

