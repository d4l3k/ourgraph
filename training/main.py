import multiprocessing

import torch
from torch import nn
from torch import optim
from adamW import AdamW
from torch.utils.data import DataLoader
from tqdm import tqdm

import model
import dataset

def all_to(arr, device):
    return [a.to(device, non_blocking=True) for a in arr]

def main():
    device = torch.device('cuda')

    pool = multiprocessing.Pool(10)
    print('loading from dgraph...')
    ALL_DOCS = pool.apply(dataset.load_docs)
    ALL_USERS = pool.apply(dataset.load_users)
    print('done loading: len(docs)={} len(users)={}'.format(
    len(ALL_DOCS), len(ALL_USERS)))

    train_dataset = dataset.GraphDataset(ALL_DOCS, ALL_USERS, train=True)
    val_dataset = dataset.GraphDataset(ALL_DOCS, ALL_USERS, train=False)
    assert len(train_dataset) > len(val_dataset)

    m = model.Model(num_docs=len(ALL_DOCS)).to(device)
    print(m)

    bs = 16
    num_workers = 16
    train_loader = DataLoader(train_dataset, batch_size=bs, shuffle=True,
            num_workers=num_workers, worker_init_fn=train_dataset.init,
            collate_fn=dataset.graph_collate, pin_memory=False)
    val_loader = DataLoader(val_dataset, batch_size=bs, shuffle=False,
            num_workers=num_workers, worker_init_fn=val_dataset.init,
            collate_fn=dataset.graph_collate, pin_memory=False)


    #criterion = nn.MSELoss()
    criterion = nn.CosineEmbeddingLoss(margin=0.5)
    #optimizer = optim.Adam(m.parameters(), lr=3e-4)
    optimizer = optim.SGD(m.parameters(), lr=0.001)

    for epoch in range(1000):
        running_loss = 0
        running_accuracy = 0
        count = 0

        progress = tqdm(train_loader)
        for i, (a, b, liked) in enumerate(progress):
            a = all_to(a, device)
            b = all_to(b, device)
            liked = liked.to(device, non_blocking=True)
            optimizer.zero_grad()
            a_vec = m(*a)
            b_vec = m(*b)
            loss = criterion(a_vec, b_vec, liked)
            loss.backward()
            optimizer.step()
            with torch.no_grad():
                similarity = (a_vec*b_vec).sum(-1)
                running_loss += loss
                running_accuracy += (similarity.round() == liked.round()).float().sum()
                count += len(liked)
                progress.set_description(
                        "epoch {}: loss={:.4f} accuracy={:.3f}".format(epoch,
                            running_loss/count, running_accuracy/count),
                        refresh=False)
                if i % 100 == 0:
                    print(liked, similarity)

            if i == 1000:
                break


    #a = FastTextEmbeddingBag("crawl-300d-2M-subword.bin")
    #print(a(['foo', 'bar']))

if __name__ == '__main__':
    main()
