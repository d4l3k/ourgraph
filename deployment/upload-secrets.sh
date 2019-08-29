#!/bin/bash

set -x

now secrets rm dgraph-crt
now secrets rm dgraph-key
now secrets rm dgraph-ca

set -e

HOST=root@ourgraph-db.fn.lc

now secrets add dgraph-crt -- "$(ssh $HOST cat /data/client.user.crt)"
now secrets add dgraph-key -- "$(ssh $HOST cat /data/client.user.key)"
now secrets add dgraph-ca -- "$(ssh $HOST cat /data/ca.crt)"
