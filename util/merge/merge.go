//package main

package merge

import (
	"fmt"
	"npmagent/common"
	"npmagent/util/netstats"
	"sync"

	"github.com/whatap/golib/net/oneway"
)

type MergeMap struct {
	mergeTagMap     []map[common.MergeKey]*common.MetricsData
	tagIndex        int
	mergeMapLock    sync.Mutex
	mergeTagMapLock []sync.Mutex
}

func NewMergeMap() *MergeMap {
	mergeMap := &MergeMap{}

	//mergeMap.index = 0
	//mergeMap.mergeMap[0] = make(map[MergeKey]*MergeData)
	mergeMap.tagIndex = 0
	mergeMap.mergeTagMap = make([]map[common.MergeKey]*common.MetricsData, 0)

	mergeMap.mergeMapLock = sync.Mutex{}
	mergeMap.mergeTagMapLock = make([]sync.Mutex, 0)

	return mergeMap
}

/*
func (mm *MergeMap) Put(key MergeKey, data *MergeData) {
	mm.mergeMapLock.Lock()
	mergeMap := mm.mergeMap[mm.index]
	if mData, ok := mergeMap[key]; ok {
		mData.SendByte += data.SendByte
		mData.SendCount += data.SendCount
		mData.RecvByte += data.RecvByte
		mData.RecvCount += data.RecvCount

		mData.Hll.Merge(data.Hll)
		mData.RowCount += 1
		mergeMap[key] = mData
	} else {
		data.RowCount = 1
		mergeMap[key] = data
	}
	mm.mergeMapLock.Unlock()
}
*/

func (mm *MergeMap) AddMap() {
	mm.mergeTagMap = append(mm.mergeTagMap, make(map[common.MergeKey]*common.MetricsData))
	mm.mergeTagMapLock = append(mm.mergeTagMapLock, sync.Mutex{})
}

func (mm *MergeMap) PutTag(i int, key common.MergeKey, data *common.MetricsData) {
	mm.mergeTagMapLock[i].Lock()
	mergeMap := mm.mergeTagMap[i]
	if mData, ok := mergeMap[key]; ok {
		mData.SendByte += data.SendByte
		mData.SendCount += data.SendCount
		mData.RecvByte += data.RecvByte
		mData.RecvCount += data.RecvCount

		mData.Hll = mData.Hll.Merge(data.Hll)
		mData.RowCount += 1
		mergeMap[key] = mData
	} else {
		data.RowCount = 1
		mergeMap[key] = data
	}
	mm.mergeTagMapLock[i].Unlock()

}

/*
func (mm *MergeMap) Switch() {
	mergeMap := make(map[MergeKey]*common.MetricsData)
	nextIndex := mm.index ^ 1

	mm.mergeMap[nextIndex] = mergeMap
	mm.index = nextIndex
}
*/

type IterFunc func(k common.MergeKey, v *common.MetricsData, netstats *netstats.Netstats, td *common.TagData, listMap map[common.TagKey]*common.TagList)
type IterTagListFunc func(k common.TagKey, v *common.TagList, tld *common.TagListData)

func Iterator(mergeMap map[common.MergeKey]*common.MetricsData, f IterFunc, pcode int64, netstats *netstats.Netstats, mapping map[common.TagKey]string, domainMap map[common.TagKey]string) {
	td := common.NewTagData()
	tld := common.NewTagListData()
	listMap := make(map[common.TagKey]*common.TagList)
	for k, v := range mergeMap {
		f(k, v, netstats, td, listMap)
	}

	for k, v := range listMap {
		tld.Add(&k, v)
	}

	for k, v := range mapping {
		d := &common.TagList{ProcessType: v, AppName: v, HostName: v, LocalInBound: 0}
		tld.Add(&k, d)
	}

	for k, v := range domainMap {
		tagList := &common.TagList{ProcessType: v, AppName: v, HostName: v, LocalInBound: 0}
		tld.Add(&k, tagList)
	}

	tld.SendTagListData(pcode)
	td.SendTagData(pcode)
}

func IteratorTagList(listMap map[common.TagKey]*common.TagList, onewayClient *oneway.OneWayTcpClient, pcode int64, mapping map[common.TagKey]string) {
	tld := common.NewTagListData()
	for k, v := range listMap {
		tld.Add(&k, v)
	}

	for k, v := range mapping {
		d := &common.TagList{ProcessType: v, AppName: v, HostName: v, LocalInBound: 0}
		tld.Add(&k, d)
	}
	tld.SendTagListData(pcode)
}

// func (mm *MergeMap) SwitchAndIterator() {
/*
func (mm *MergeMap) SwitchAndIterator(f IterFunc, onewayClient *oneway.OneWayTcpClient, pcode int64) {
	mergeMap := make(map[MergeKey]*MergeData)
	nowIndex := mm.index
	nextIndex := mm.index ^ 1

	mm.mergeMap[nextIndex] = mergeMap
	mm.index = nextIndex

	mergeMap = mm.mergeMap[nowIndex]
	mm.mergeMap[nowIndex] = nil

	mm.mergeMapLock.RLock()
	go Iterator(mergeMap, f, onewayClient, pcode)
	mm.mergeMapLock.RUnlock()
}
*/

func (mm *MergeMap) SwitchAndIteratorTag(f IterFunc, pcode int64, netstats *netstats.Netstats, mapping map[common.TagKey]string, domainMap map[common.TagKey]string) {
	mergeMap := make(map[common.MergeKey]*common.MetricsData)
	for i, m := range mm.mergeTagMap {
		newMap := make(map[common.MergeKey]*common.MetricsData)
		mm.mergeTagMapLock[i].Lock()
		mm.mergeTagMap[i] = newMap
		mm.mergeTagMapLock[i].Unlock()

		for k, v := range m {
			if mData, ok := mergeMap[k]; ok {
				mData.SendByte += v.SendByte
				mData.SendCount += v.SendCount
				mData.RecvByte += v.RecvByte
				mData.RecvCount += v.RecvCount

				mData.Hll = mData.Hll.Merge(v.Hll)
				mData.RowCount += 1
				mergeMap[k] = mData
			} else {
				v.RowCount = 1
				mergeMap[k] = v
			}
		}

	}

	go Iterator(mergeMap, f, pcode, netstats, mapping, domainMap)
}

func main() {
	fmt.Println("vim-go")
}
