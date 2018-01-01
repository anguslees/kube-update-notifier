FROM golang:1.9-alpine3.7 as gobuild
WORKDIR /go/src/github.com/anguslees/kube-update-notifier
COPY . .
RUN go install .

FROM alpine:3.7
MAINTAINER Angus Lees <gus@inodes.org>
RUN apk --no-cache add ca-certificates
COPY --from=gobuild /go/bin/kube-update-notifier /usr/bin/
CMD ["kube-update-notifier"]
