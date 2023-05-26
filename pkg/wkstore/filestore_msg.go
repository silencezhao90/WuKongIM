package wkstore

import (
	"fmt"
	"sync"

	"github.com/WuKongIM/WuKongIM/pkg/wklog"
	"github.com/WuKongIM/WuKongIM/pkg/wkutil"
	lru "github.com/hashicorp/golang-lru/v2"
)

var segmentCache *lru.Cache[string, *segment]

func init() {
	var err error
	segmentCache, err = lru.NewWithEvict(100, func(key string, value *segment) {
		value.release()
	})
	if err != nil {
		panic(err)
	}
}

type FileStoreForMsg struct {
	cfg     *StoreConfig
	slotMap map[uint32]*slot
	wklog.Log
	slotMapLock sync.RWMutex
}

func NewFileStoreForMsg(cfg *StoreConfig) *FileStoreForMsg {

	f := &FileStoreForMsg{
		cfg:     cfg,
		slotMap: make(map[uint32]*slot),
		Log:     wklog.NewWKLog("FileStoreForMsg"),
	}
	return f
}

func (f *FileStoreForMsg) AppendMessages(channelID string, channelType uint8, msgs []Message) (seqs []uint32, err error) {
	seqs, _, err = f.getTopic(channelID, channelType).appendMessages(msgs)
	return
}

func (f *FileStoreForMsg) LoadMsg(channelID string, channelType uint8, messageSeq uint32) (Message, error) {
	return f.getTopic(channelID, channelType).readMessageAt(messageSeq)
}

func (f *FileStoreForMsg) LoadLastMsgs(channelID string, channelType uint8, limit int) ([]Message, error) {
	var messages = make([]Message, 0, limit)
	tp := f.getTopic(channelID, channelType)
	err := tp.readLastMessages(uint64(limit), func(message Message) error {
		messages = append(messages, message)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return messages, nil
}

func (f *FileStoreForMsg) LoadLastMsgsWithEnd(channelID string, channelType uint8, endMessageSeq uint32, limit int) ([]Message, error) {
	var messages = make([]Message, 0, limit)
	tp := f.getTopic(channelID, channelType)
	err := tp.readLastMessages(uint64(limit), func(message Message) error {
		if endMessageSeq != 0 && message.GetSeq() <= endMessageSeq {
			return nil
		}
		messages = append(messages, message)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return messages, nil
}

func (f *FileStoreForMsg) LoadPrevRangeMsgs(channelID string, channelType uint8, startMessageSeq, endMessageSeq uint32, limit int) ([]Message, error) {
	if startMessageSeq == 0 {
		return nil, fmt.Errorf("start messageSeq must be greater than 0")
	}
	actLimit := limit
	var actStartMessageSeq uint32 = 0
	if startMessageSeq < uint32(limit) {
		actLimit = int(startMessageSeq)
		actStartMessageSeq = 0
	} else {
		actStartMessageSeq = startMessageSeq - uint32(limit) + 1
	}

	tp := f.getTopic(channelID, channelType)
	var messages = make([]Message, 0, limit)
	err := tp.readMessages(actStartMessageSeq, uint64(actLimit), func(message Message) error {
		if endMessageSeq != 0 && message.GetSeq() <= endMessageSeq {
			return nil
		}
		messages = append(messages, message)
		return nil
	})
	return messages, err
}

func (f *FileStoreForMsg) LoadNextRangeMsgs(channelID string, channelType uint8, startMessageSeq, endMessageSeq uint32, limit int) ([]Message, error) {
	var messages = make([]Message, 0, limit)
	tp := f.getTopic(channelID, channelType)
	err := tp.readMessages(startMessageSeq, uint64(limit), func(message Message) error {
		if endMessageSeq != 0 && message.GetSeq() >= endMessageSeq {
			return nil
		}
		messages = append(messages, message)
		return nil
	})
	return messages, err
}

func (f *FileStoreForMsg) DeleteChannelAndClearMessages(channelID string, channelType uint8) error {
	f.Warn("暂未实现DeleteChannelAndClearMessages")
	return nil
}

func (f *FileStoreForMsg) Close() error {
	if len(f.slotMap) == 0 {
		return nil
	}
	for _, s := range f.slotMap {
		s.close()
	}
	return nil
}

func (f *FileStoreForMsg) topicName(channelID string, channelType uint8) string {
	return fmt.Sprintf("%d-%s", channelType, channelID)
}

func (f *FileStoreForMsg) getTopic(channelID string, channelType uint8) *topic {
	topic := f.topicName(channelID, channelType)
	slotNum := wkutil.GetSlotNum(f.cfg.SlotNum, topic)
	f.slotMapLock.RLock()
	slot := f.slotMap[slotNum]
	f.slotMapLock.RUnlock()
	if slot == nil {
		slot = newSlot(slotNum, f.cfg)
		f.slotMapLock.Lock()
		f.slotMap[slotNum] = slot
		f.slotMapLock.Unlock()
	}
	return slot.getTopic(topic)
}