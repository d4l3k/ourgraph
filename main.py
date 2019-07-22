from typing import List
import json

from cityhash import CityHash64
from grpc._cython import cygrpc
import torch
from torch.nn import EmbeddingBag
from torch.utils.data import Dataset, DataLoader
import fasttext
import numpy as np
import pydgraph


DGRAPH_STUB = pydgraph.DgraphClientStub(
    'localhost:9080',
    options=[
        (cygrpc.ChannelArgKey.max_send_message_length, -1),
        (cygrpc.ChannelArgKey.max_receive_message_length, -1)
    ],
)
DGRAPH = pydgraph.DgraphClient(DGRAPH_STUB)


class FastTextEmbeddingBag(EmbeddingBag):
    def __init__(self, model_path: str):
        self.model = fasttext.load_model(model_path)
        input_matrix = self.model.get_input_matrix()
        input_matrix_shape = input_matrix.shape
        super().__init__(input_matrix_shape[0], input_matrix_shape[1])
        self.weight.data.copy_(torch.tensor(input_matrix, dtype=torch.float))

    def forward(self, words: List[str]) -> torch.Tensor:
        word_subinds = np.empty([0], dtype=np.int64)
        word_offsets = [0]
        for word in words:
            _, subinds = self.model.get_subwords(word)
            word_subinds = np.concatenate((word_subinds, subinds))
            word_offsets.append(word_offsets[-1] + len(subinds))
        word_offsets = word_offsets[:-1]
        ind = torch.tensor(word_subinds, dtype=torch.long)
        offsets = torch.tensor(word_offsets, dtype=torch.long)
        return super().forward(ind, offsets)


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

a = FastTextEmbeddingBag("crawl-300d-2M-subword.bin")
print(a(['foo', 'bar']))
