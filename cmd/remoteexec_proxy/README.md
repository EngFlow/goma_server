# Goma server - remoteexec_proxy

# How to deploy it on Google App Engine Flexible Environment

`remoteexec_proxy` can run on Google App Engine Flexible Environment.

## Dependencies

Need to setup [cloud memorystore](https://cloud.google.com/memorystore/)
and [cloud storage](https://cloud.google.com/storage/).

### Cloud Memorystore

Cloud memorystore is used for digest cache.
You need to create an instance in the same location as App Engine apps.

NOTE If different location, your server will get many `context deadline exceeded` errors.

If not created, run `gcloud app create` can create App Engine app in
the specific region in the cloud project.

```
$ gcloud app create --region=$REGION
```

If it is already created, you can check it by `gcloud app describe`

```
$ gcloud app describe --format='get(locationId)'
```

It is sufficient with 1GB size, but you might want to set
`maxmemory-policy: allkeys-lru`.

```
$ gcloud redis instances create $REDIS_INSTANCE_NAME \
 --region=$REGION \
 --redis-config='maxmemory-poclicy=allkeys-lru'
```

### Cloud Storage

Cloud storage is used to store file cache.

```
$ gsutil mb gs://${PROJECT_ID}-file-cache
$ gsutil lifecycle set '{"rule": [{"action": {"type": "Delete"}, "condition": {"age": 1}}]}' gs://${PROJECT_ID}-file-cache
```

## Deploy

To deploy the server, create a workspace

```
$ mkdir /path/to/workspace
$ cd /path/to/workspace
$ TOPDIR=$(pwd)
$ git clone https://chromium.googlesource.com/infra/goma/server
$ cd server
$ GO111MODULE=on go mod vendor
$ mkdir ../gopath/src
$ export GOPATH=$(TOPDIR)/gopath
$ mv vendor/* ../gopath/src
$ mkdir app
$ cd app
$ cp ../server/cmd/remoteexec_server/* .
$ REDISHOST=$(gcloud redis instances describe $REDIS_INSTANCE_NAME \
   --region $REGION --format='get(host)')
$ REDISPORT=$(gcloud redis instances describe $REDIS_INSTANCE_NAME \
   --region $REGION --format='get(port)')
$ cat > app.yaml <<EOF
runtime: go
env: flex

network:
  name: default

liveness_check:
  path: "/healthz"

readiness_check:
  path: "/healthz"

env_variables:
  REDISHOST: "$REDISHOST"
  REDISPORT: "$REDISPORT"
EOF
$ cat > flag.go << EOF
package main

func init() {
	*port = 8080
	*remoteexecAddr = "remotebuildexecution.googleapis.com:443"
	*remoteInstanceName = "projects/$PROJECT_ID/instances/default_instance"
	*whitelistedUsers = "<comma separated whitelisted-users-email-address>"
	*platformContainerImage = "docker://gcr.io/..."
	*fileCacheBucket = "$PROJECT_ID-file-cache"
}
EOF
$ gcloud app deploy
```

Then, you can use `$PROJECT_ID.appspot.com` as `$GOMA_SERVER_HOST`.
You need to specify `GOMA_ARBTRARY_TOOLCHAIN_SUPPORT=true`.

