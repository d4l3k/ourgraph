from typing import List
import json

import torch
import pydgraph
from torch.utils.data import Dataset, DataLoader
from grpc._cython import cygrpc
from cityhash import CityHash64
import fasttext


DGRAPH_STUB = pydgraph.DgraphClientStub(
    'localhost:9080',
    options=[
        (cygrpc.ChannelArgKey.max_send_message_length, -1),
        (cygrpc.ChannelArgKey.max_receive_message_length, -1)
    ],
)
DGRAPH = pydgraph.DgraphClient(DGRAPH_STUB)


def filter_uids(uids: List[str], train: bool) -> List[str]:
    """
    filter_uids returns 5% of the uids if train is false. 95% otherwise.
    uses hashing to remain consistent
    """
    return [
        uid for uid in uids
        if (CityHash64(uid) % 20 == 0) != train
    ]


def docs() -> List[str]:
    """
    docs returns all document UIDs
    """
    resp = DGRAPH.txn(read_only=True).query(
        """{
            docs(func: has(url)) {
                uid
            }
        }""",
    )
    res = json.loads(resp.json)
    return [doc["uid"] for doc in res["docs"]]


def users() -> List[str]:
    """
    users returns all user UIDs
    """
    resp = DGRAPH.txn(read_only=True).query(
        """{
            users(func: has(username)) {
                uid
            }
        }""",
    )
    res = json.loads(resp.json)
    return [user["uid"] for user in res["users"]]


ALL_DOCS = docs()
ALL_USERS = users()


class GraphDataset(Dataset):
    """
    GraphDataset returns a triplet of (doc, connected doc, disconnected doc).
    """
    def __init__(self, train: bool):
        self.train: bool = train
        self.docs: List[str] = ALL_DOCS
        self.users: List[str] = filter_uids(ALL_USERS, train)

    def __len__(self) -> int:
        return len(self.users)

    def __getitem__(self, i):
        pass


train_dataset = GraphDataset(train=True)
val_dataset = GraphDataset(train=False)
assert len(train_dataset) > len(val_dataset)
