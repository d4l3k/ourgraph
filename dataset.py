import torch
from torch.utils.data import Dataset
from typing import List, Dict
import pydgraph
from grpc._cython import cygrpc
import json
import cityhash
import random
from model import tag_embed_idx
from torch.utils.data._utils.collate import default_collate


def dgraph_client():
    stub = pydgraph.DgraphClientStub(
        'localhost:9080',
        options=[
            (cygrpc.ChannelArgKey.max_send_message_length, -1),
            (cygrpc.ChannelArgKey.max_receive_message_length, -1)
        ],
    )
    return pydgraph.DgraphClient(stub)

def filter_uids(uids: List[str], train: bool) -> List[str]:
    """
    filter_uids returns 5% of the uids if train is false. 95% otherwise.
    uses hashing to remain consistent
    """
    return [
        uid for uid in uids
        if (cityhash.CityHash64(uid) % 20 == 0) != train
    ]


def load_docs() -> List[str]:
    """
    docs returns all document UIDs
    """
    resp = dgraph_client().txn(read_only=True).query(
        """{
            docs(func: has(url)) {
                uid
            }
        }""",
    )
    res = json.loads(resp.json)
    docs = [doc["uid"] for doc in res["docs"]]
    return docs


def load_users() -> List[str]:
    """
    users returns all user UIDs
    """
    resp = dgraph_client().txn(read_only=True).query(
        """{
            users(func: gt(count(likes), 1)) {
                uid
            }
        }""",
    )
    res = json.loads(resp.json)
    return [user["uid"] for user in res["users"]]


def cum_offsets(tag_offsets):
    t = torch.zeros(len(tag_offsets), dtype=torch.long)
    cum = 0
    for i, v in enumerate(tag_offsets):
        t[i] = cum
        cum += v
    return t


def collate_docs(docs):
    dense, doc_ids, tag_ids, tag_offsets = zip(*docs)
    return (
        default_collate(dense),
        default_collate(doc_ids),
        torch.cat(tag_ids),
        cum_offsets(tag_offsets),
    )


def graph_collate(batch):
    a, b, liked = zip(*batch)
    return (
        collate_docs(a),
        collate_docs(b),
        default_collate(liked),
    )


class GraphDataset(Dataset):
    """
    GraphDataset returns a triplet of (doc, connected doc, disconnected doc).
    """
    def __init__(self, docs: List[str], users: List[str], train: bool):
        super().__init__()

        self.train: bool = train
        self.docs: List[str] = docs
        self.users: List[str] = filter_uids(users, train)
        self.doc_idx: Dict[str, int] = {v: i for i, v in enumerate(docs)}

    def __len__(self) -> int:
        return len(self.users)

    def __getitem__(self, i):
        docs = self.get_user(self.users[i])["likes"]

        liked = 1 if random.randint(0, 1) else -1
        a = self.transform_doc(random.choice(docs))
        if liked == 1:
            b = self.transform_doc(random.choice(docs))
        else:
            b = self.transform_doc(self.get_doc(random.choice(self.docs)))
        return a, b, torch.tensor(liked, dtype=torch.float)

    def transform_doc(self, doc):
        dense = torch.FloatTensor([
            doc.get("wordcount", 0),
            doc.get("reviews", 0),
            doc.get("chapters", 0),
            doc.get("likecount", 0),
            doc.get("complete", False),
        ])

        doc_ids = torch.LongTensor([self.doc_idx[doc["uid"]]])
        tag_ids = torch.LongTensor([
            tag_embed_idx(tag) for tag in doc["tags"]
        ])
        tag_offsets = torch.LongTensor([len(tag_ids)])
        return (dense, doc_ids, tag_ids, tag_offsets)

    def get_user(self, uid: str) -> dict:
        resp = self.dgraph.txn(read_only=True).query(
            """query user($user: string) {
                user(func: uid($user)) {
                    likes {
                        uid
                        wordcount
                        reviews
                        chapters
                        likecount
                        tags
                        title
                        complete
                        desc
                    }
                }
            }""",
            variables={"$user": uid},
        )
        users = json.loads(resp.json)["user"]
        if len(users) != 1:
            print("duplicate users! uid = {}".format(uid))
        return users[0]

    def init(self, *args):
        self.dgraph = dgraph_client()

    def get_doc(self, uid: str) -> dict:
        resp = self.dgraph.txn(read_only=True).query(
            """query doc($doc: string) {
                doc(func: uid($doc)) {
                    uid
                    wordcount
                    reviews
                    chapters
                    likecount
                    tags
                    title
                    complete
                    desc
                }
            }""",
            variables={"$doc": uid},
        )
        docs = json.loads(resp.json)["doc"]
        assert len(docs) == 1
        return docs[0]
