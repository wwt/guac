# guac

A port of the [Apache Guacamole client](https://github.com/apache/guacamole-client) to Go.

Apache Guacamole provides access to your desktop using remote desktop protocols in your web browser without any plugins.

[![GoDoc](https://godoc.org/github.com/wwt/guac?status.svg)](http://godoc.org/github.com/wwt/guac)
[![Go Report Card](https://goreportcard.com/badge/github.com/wwt/guac)](https://goreportcard.com/report/github.com/wwt/guac)
[![Build Status](https://travis-ci.org/wwt/guac.svg?branch=master)](https://travis-ci.org/wwt/guac)

## Development

First start guacd in a container, for example:

```sh
docker run --name guacd -d -p 4822:4822 guacamole/guacd
```

Next run the example main:

```sh
go run cmd/guac/guac.go
```

Now you can connect with [the example Vue app](https://github.com/wwt/guac-vue).  By default, guac will try to connect to a guacd instance at `127.0.0.1:4822`.  If you need to configure something different, you can do so by configuring environment variables; see the configurable parameters below.

Guac listens on `http://0.0.0.0:4567`.  If you have a need for the connection to Guac to be secure, you will need to pass a certificate and keyfile to it using the `CERT_PATH` and `CERT_KEY_PATH` environment variables; it will then listen on `https://0.0.0.0:4567`.  The secure connection uses TLS 1.3.

## Configurable parameters
| Environment Variable | Description                                                                                              | Default Value  | Required? |
| -------------------- | -------------------------------------------------------------------------------------------------------- | -------------- | ----------|
| `CERT_PATH`          | Full path, including filename, to a certificate file in order for guac to listen on HTTPS (TLS 1.3)      |                | No        |
| `CERT_KEY_PATH`      | Full path, including filename, to the certificate keyfile in order for guac to listen on HTTPS (TLS 1.3) |                | No        |
| `GUACD_ADDRESS`      | The address and port that guacd is listening on                                                          | 127.0.0.1:4822 | No        |

## Acknowledgements

Initially forked from https://github.com/johnzhd/guacamole_client_go which is a direct rewrite of the Java Guacamole
client. This project no longer resembles that one but it helped it get off the ground!

Some of the comments are taken directly from the official Apache Guacamole Java client.
