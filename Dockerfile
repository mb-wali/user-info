FROM golang:1.15-alpine

RUN apk add --no-cache git
RUN go get -u github.com/jstemmer/go-junit-report

COPY . /go/src/github.com/cyverse-de/user-info

ENV CGO_ENABLED=0
RUN go install -v github.com/cyverse-de/user-info

ENTRYPOINT ["user-info"]
CMD ["--help"]
EXPOSE 60001

ARG git_commit=unknown
ARG version="2.9.0"
ARG descriptive_version=unknown

LABEL org.cyverse.git-ref="$git_commit"
LABEL org.cyverse.version="$version"
LABEL org.cyverse.descriptive-version="$descriptive_version"
LABEL org.label-schema.vcs-ref="$git_commit"
LABEL org.label-schema.vcs-url="https://github.com/cyverse-de/user-info"
LABEL org.label-schema.version="$descriptive_version"


### Build
# docker build -t mbwali/user-info:latest .

### Run
# docker run -it mbwali/user-info -p 60001:60001


# config
# config", "/etc/iplant/de/jobservices.yml", "The path to the config file


# GET
# http://localhost:60001/bags/default/username
