FROM golang:1.15
COPY . /app
WORKDIR /app
RUN go build -o go-junit2immudb
ENTRYPOINT [ "./go-junit2immudb" ]