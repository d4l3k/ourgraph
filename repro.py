import torch
from torch.utils.data import Dataset, DataLoader

import pydgraph

def dgraph_client():
    stub = pydgraph.DgraphClientStub('localhost:9080')
    return pydgraph.DgraphClient(stub)


class GraphDataset(Dataset):
    def __init__(self):
        super().__init__()

        self.docs = list(range(100))

    def __len__(self) -> int:
        return len(self.docs)

    def __getitem__(self, i):
        resp = dgraph_client().txn(read_only=True).query(
            """{
                user(func: has(username), first: 1) {
                   uid
                   username
                }
            }""",
        )
        print(resp)
        return torch.tensor([i])


train_dataset = GraphDataset()
train_dataset[0] # removing this line fixes this code

train_loader = DataLoader(train_dataset, batch_size=8, num_workers=8)
# running multiple dgraph requests in parallel causes the crash
print(next(iter(train_loader)))
