package main

import (
	"log"
	"math/rand"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	torch "github.com/d4l3k/gotorch"
	"github.com/paulbellamy/ratecounter"
	"golang.org/x/sync/errgroup"
)

type Edge struct {
	Left, Right int
}

func makeEmbeddingTable(n, dim int) []EmbeddingEntry {
	dim64 := int64(dim)
	table := make([]EmbeddingEntry, n)
	for i := range table {
		t := torch.RandN(dim64)
		t.SetRequiresGrad(true)
		table[i].Tensor = t
	}
	return table
}

type EmbeddingEntry struct {
	sync.Mutex

	Tensor *torch.Tensor
}

type Model struct {
	// Documents is the document embedding table.
	Documents []EmbeddingEntry
	LR        float32
	BatchSize int
	Pattern   *torch.Tensor

	QPS *ratecounter.RateCounter
}

func (m *Model) worker() error {
	return nil
}

func lockAll(table []EmbeddingEntry, ids []int) {
	for _, id := range ids {
		table[id].Lock()
	}
}

func unlockAll(table []EmbeddingEntry, ids []int) {
	for _, id := range ids {
		table[id].Unlock()
	}
}

func (m *Model) trainBatch(i int, batch []Edge) error {
	tensors := make([]*torch.Tensor, 0, len(batch))
	tensorSet := map[int]struct{}{}
	tensorIDs := make([]int, 0, len(batch))

	maybeAddTensor := func(i int) {
		if _, ok := tensorSet[i]; !ok {
			tensors = append(tensors, m.Documents[i].Tensor)
			tensorIDs = append(tensorIDs, i)
			tensorSet[i] = struct{}{}
		}
	}

	negatives := make([]*torch.Tensor, len(batch))
	for i, edge := range batch {
		negative := rand.Int() % len(m.Documents)
		maybeAddTensor(edge.Left)
		maybeAddTensor(edge.Right)
		maybeAddTensor(negative)
		negatives[i] = m.Documents[negative].Tensor
	}

	opt := torch.Adam(tensors, m.LR)

	// Lock the IDs in order to avoid deadlocking.
	sort.Ints(tensorIDs)
	lockAll(m.Documents, tensorIDs)
	defer unlockAll(m.Documents, tensorIDs)

	opt.ZeroGrad()

	scores := make([]*torch.Tensor, 0, 2*len(batch))

	for i, edge := range batch {
		left := m.Documents[edge.Left].Tensor
		right := m.Documents[edge.Right].Tensor
		random := negatives[i]
		positive := left.Dot(right)
		negative := left.Dot(random)
		scores = append(scores, positive, negative)
	}

	out := torch.Stack(0, scores...)
	loss := torch.MSELoss(out, m.Pattern)
	loss.Backward()

	opt.Step()

	if i%1000 == 0 {
		log.Printf("loss = %+v, qps = %+v", loss.Blob(), m.QPS.Rate()/10)
		debug.FreeOSMemory()
	}

	m.QPS.Incr(int64(len(batch)))

	return nil
}

func run() error {
	numDocs := 100
	const embeddingDim = 100
	model := Model{
		LR:        0.001,
		Documents: makeEmbeddingTable(numDocs, embeddingDim),
		BatchSize: 2,
		QPS:       ratecounter.NewRateCounter(10 * time.Second),
	}
	vals := []float32{}
	for i := 0; i < model.BatchSize; i++ {
		vals = append(vals, 1, 0)
	}
	pattern, err := torch.TensorFromBlob(vals, []int64{int64(model.BatchSize * 2)})
	if err != nil {
		return err
	}
	model.Pattern = pattern

	var eg errgroup.Group
	const numWorkers = 16
	for w := 0; w < numWorkers; w++ {
		eg.Go(func() error {
			for i := 0; i < 1000000; i++ {
				batch := []Edge{
					{1, 2},
					{3, 2},
				}
				if err := model.trainBatch(i, batch); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return eg.Wait()
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("%+v", err)
	}
}
