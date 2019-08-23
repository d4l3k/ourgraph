#!/bin/bash

set -x

now secrets rm dgraph-crt
now secrets rm dgraph-key

set -e

HOST=root@ourgraph-db.fn.lc

now secrets add dgraph-crt -- "$(ssh $HOST cat /data/client.user.crt)"
now secrets add dgraph-key -- "$(ssh $HOST cat /data/client.user.key)"
