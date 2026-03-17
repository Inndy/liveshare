liveshare: *.go **/*.go
	go build -trimpath -ldflags '-w -s' -o $@ .

liveshare.exe: *.go **/*.go
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '-w -s' -o $@ .
	strip $@
