package protocol

import (
	"github.com/idena-network/idena-go/common"
	"github.com/idena-network/idena-go/common/pushpull"
	"github.com/idena-network/idena-go/log"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/patrickmn/go-cache"
	"sync"
	"sync/atomic"
	"time"
)

type pendingPush struct {
	cnt  uint32
	hash pushPullHash
}

type pullRequest struct {
	peer peer.ID
	hash pushPullHash
}

type PushPullManager struct {
	pendingPushes    *cache.Cache
	mutex            sync.Mutex
	requests         chan pullRequest
	entryHolders     map[pushType]pushpull.Holder
	throttlingLogger log.ThrottlingLogger
}

func NewPushPullManager() *PushPullManager {
	return &PushPullManager{
		pendingPushes:    cache.New(time.Minute*3, time.Minute*5),
		requests:         make(chan pullRequest, 5000),
		entryHolders:     make(map[pushType]pushpull.Holder),
		throttlingLogger: log.NewThrottlingLogger(log.New("component", "pushPullManager")),
	}
}

func (m *PushPullManager) AddEntryHolder(pushId pushType, holder pushpull.Holder) {
	m.entryHolders[pushId] = holder
}

func (m *PushPullManager) addPush(id peer.ID, hash pushPullHash) {

	key := hash.String()

	holder := m.entryHolders[hash.Type]
	if holder == nil {
		panic("pushpull holder is not found")
	}
	if holder.Has(hash.Hash) {
		return
	}

	_, ok := m.pendingPushes.Get(key)

	if !ok {
		m.mutex.Lock()
		_, ok = m.pendingPushes.Get(key)
		if !ok {
			m.pendingPushes.SetDefault(key, &pendingPush{
				cnt:  1,
				hash: hash,
			})
			m.makeRequest(id, hash)
			if holder.SupportPendingRequests() {
				holder.PushTracker().RegisterPull(hash.Hash)
			}
			m.mutex.Unlock()
			return
		}
		m.mutex.Unlock()
	}
	value, _ := m.pendingPushes.Get(key)

	pendingPush := value.(*pendingPush)
	cnt := atomic.AddUint32(&pendingPush.cnt, 1)
	if cnt >= holder.MaxParallelPulls() {
		if holder.SupportPendingRequests() {
			holder.PushTracker().AddPendingPush(id, hash.Hash)
		}
		return
	}
	m.makeRequest(id, hash)
	if holder.SupportPendingRequests() {
		holder.PushTracker().RegisterPull(hash.Hash)
	}
}

func (m *PushPullManager) makeRequest(peer peer.ID, hash pushPullHash) {
	select {
	case m.requests <- pullRequest{peer: peer, hash: hash}:
	default:
		m.throttlingLogger.Warn("Pull request skipped")
	}
}

func (m *PushPullManager) AddEntry(key pushPullHash, entry interface{}, shardId common.ShardId, highPriority bool) {
	m.entryHolders[key.Type].Add(key.Hash, entry, shardId, highPriority)
}

func (m *PushPullManager) GetEntry(hash pushPullHash) (interface{}, common.ShardId, bool, bool) {
	return m.entryHolders[hash.Type].Get(hash.Hash)
}

func (m *PushPullManager) Requests() chan pullRequest {
	return m.requests
}

func (m *PushPullManager) Run() {
	for entryType, holder := range m.entryHolders {
		if holder.SupportPendingRequests() {
			go m.loop(entryType, holder)
		}
	}
}

func (m *PushPullManager) loop(entryType pushType, holder pushpull.Holder) {
	for {
		req := <-holder.PushTracker().Requests()
		m.makeRequest(req.Id, pushPullHash{
			Type: entryType,
			Hash: req.Hash,
		})
		holder.PushTracker().RegisterPull(req.Hash)
	}
}
