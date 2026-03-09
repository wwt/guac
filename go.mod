module github.com/wwt/guac

go 1.20

replace github.com/Sirupsen/logrus v1.4.2 => github.com/sirupsen/logrus v1.4.2

require (
	github.com/google/uuid v1.3.0
	github.com/gorilla/websocket v1.5.0
	github.com/sirupsen/logrus v1.9.1
)

require golang.org/x/sys v0.7.0 // indirect
