# go-junit2immudb

Inspired by https://github.com/SimoneLazzaris/stdin2immudb and https://github.com/joshdk/go-junit
The purpose of this project is to provide a simple tool to store test results immutably.

## Why

Test evidence artifacts such as JUnit reports are widely used and consumed in different ways. Storing this in a tamperproof database ensures that your artifacts are not modified, thus gives your data more reliability. Having a CLI tool that does this, helps integrate with either containerized, ephemeral or long-running immudb instances and any CI/CD environment.

## Basic usage

**Requires a running immudb instance, currently working with unreleased features @master branch, see <https://github.com/codenotary/immudb> for more information.**
Build running (Requires golang 1.12 or newer or docker and docker-compose) :

```bash
go build -o junit2immudb 

# or build with docker-compose if you're feeling lucky :)
ID="$(id -u)" GID="$(id -g)" docker-compose run builder
# The compiled binary will be available in the dist directory
```

Then after adding to your PATH environment variable or using a relative or fully qualified path, run:

```bash
./junit2immudb -filename path-to-some-junit-report -hostname {localhost/your-immudb-host} -port {3322/your-immudb-port} -username {immudb/your-immudb-username} -password {your-immudb-password}
```

## What this will do

Depending on the database configured via the ```database``` flag or using ```defaultdb``` as a fallback, this tool will create 2 tables, one for storing test suite results and one for storing individual results.


## Pending: 

* Once immudb supports new datatypes, migrate BLOBS to whatever is appropriate for the data.
* This tool does not yet read the BLOB values from the database.
  
