from typing import List, Dict
import torch
from torch import nn
import cityhash
import fasttext


TAG_EMBEDDING_SIZE = 10000
TAG_EMBEDDING_DIM = 32
DOC_EMBEDDING_DIM = 32


def tag_embed_idx(tag: str) -> int:
    return cityhash.CityHash64(tag) % TAG_EMBEDDING_SIZE


class FastTextEmbeddingBag(nn.EmbeddingBag):
    def __init__(self, model_path: str):
        self.model = fasttext.load_model(model_path)
        input_matrix = self.model.get_input_matrix()
        input_matrix_shape = input_matrix.shape
        super().__init__(input_matrix_shape[0], input_matrix_shape[1])
        self.weight.data.copy_(torch.tensor(input_matrix, dtype=torch.float))

    def forward(self, words: List[str]) -> torch.Tensor:
        word_subinds = np.zeros([0], dtype=np.int64)
        word_offsets = [0]
        for word in words:
            _, subinds = self.model.get_subwords(word)
            word_subinds = np.concatenate((word_subinds, subinds))
            word_offsets.append(word_offsets[-1] + len(subinds))
        word_offsets = word_offsets[:-1]
        ind = torch.tensor(word_subinds, dtype=torch.long)
        offsets = torch.tensor(word_offsets, dtype=torch.long)
        return super().forward(ind, offsets)


class Model(nn.Module):
    def __init__(self, num_docs: int):
        super().__init__()

        self.tag_embedding = torch.nn.EmbeddingBag(
                TAG_EMBEDDING_SIZE, TAG_EMBEDDING_DIM, mode="max")
        self.doc_embedding = torch.nn.Embedding(
                num_docs, DOC_EMBEDDING_DIM)
        self.fc1 = nn.Linear(TAG_EMBEDDING_DIM + DOC_EMBEDDING_DIM + 5, 128)
        self.fc2 = nn.Linear(128, 128)
        self.fc3 = nn.Linear(128, 64)

    def forward(self, dense, docs, tags, tag_offsets):
        tags = self.tag_embedding(tags, tag_offsets)
        docs = self.doc_embedding(docs).squeeze(dim=1)
        x = torch.cat((dense, tags, docs), dim=1)
        x = self.fc1(x)
        x = nn.functional.relu(x)
        x = self.fc2(x)
        x = nn.functional.relu(x)
        x = self.fc3(x)
        return x
