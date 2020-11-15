package types

import (
	"errors"
	"sort"
	"time"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cybercongress/go-cyber/types"
	"github.com/cybercongress/go-cyber/x/link"
)

type BaseSearchIndex struct {
	links 	   []cidLinks
	backlinks  []cidLinks
	accounts   map[types.AccNumber]map[link.CidNumber]link.CidNumber
	rank  	   Rank

	linksChan  chan link.CompactLink
	rankChan   chan Rank
	errChan    chan error

	locked     bool

	logger     log.Logger
}

func NewBaseSearchIndex(log log.Logger) *BaseSearchIndex {
	return &BaseSearchIndex{
		linksChan: make(chan link.CompactLink, 1000),
		rankChan:  make(chan Rank, 1),
		errChan:   make(chan error),
		locked:    true,
		logger:    log,
	}
}

func (i *BaseSearchIndex) Run() GetError {
	go i.startListenNewLinks()
	go i.startListenNewRank()

	return i.checkIndexError
}

// Load links with zero rank values. No sorting. Index should be unavailable for read
func (i *BaseSearchIndex) Load(links link.Links) {

	startTime := time.Now()
	i.lock() // lock index for read

	i.links = make([]cidLinks, 0, 1000000)
	i.backlinks = make([]cidLinks, 0, 1000000)
	i.accounts = make(map[types.AccNumber]map[link.CidNumber]link.CidNumber)

	for from, toCids := range links {
		i.extendIndex(uint64(from))

		for to, acc := range toCids {
			for j := range acc {
				if i.accounts[j] == nil {
					i.accounts[j] = make(map[link.CidNumber]link.CidNumber)
				}
				i.accounts[j][from] = to
				i.logger.Info("[ACC]: ", i.accounts[j])
			}
			i.putLinkIntoIndex(from, to)

			i.extendReverseIndex(uint64(to))
			i.putBacklinkIntoIndex(from, to)
		}
	}

	i.logger.Info("The node search index is loaded", "time", time.Since(startTime))
}

func (i *BaseSearchIndex) PutNewLinks(links []link.CompactLink) {
	for _, link := range links {
		i.linksChan <- link
	}
}

func (i *BaseSearchIndex) PutNewRank(rank Rank) {
	i.rankChan <- rank.CopyWithoutTree()
}

func (i *BaseSearchIndex) Search(cidNumber link.CidNumber, page, perPage int) ([]RankedCidNumber, int, error) {

	i.logger.Info("Search query", "cid", cidNumber, "page", page, "perPage", perPage)

	if i.locked {
		return nil, 0, errors.New("search index currently unavailable after node restart")
	}

	if uint64(cidNumber) >= uint64(len(i.links)) {
		return []RankedCidNumber{}, 0, nil
	}

	cidLinks := i.links[cidNumber]
	if cidLinks.sortedLinks == nil || len(cidLinks.sortedLinks) == 0 {
		return []RankedCidNumber{}, 0, nil
	}

	totalSize := len(cidLinks.sortedLinks)
	startIndex := page * perPage
	if startIndex >= totalSize {
		return nil, totalSize, errors.New("page not found")
	}

	endIndex := startIndex + perPage
	if endIndex > totalSize {
		endIndex = startIndex + (totalSize % perPage)
	}

	resultSet := cidLinks.sortedLinks[startIndex:endIndex]

	return resultSet, totalSize, nil
}

func (i *BaseSearchIndex) Backlinks(cidNumber link.CidNumber, page, perPage int) ([]RankedCidNumber, int, error) {

	i.logger.Info("Backlinks query", "cid", cidNumber, "page", page, "perPage", perPage)

	if i.locked {
		return nil, 0, errors.New("The search index is currently unavailable after node restart")
	}

	if uint64(cidNumber) >= uint64(len(i.backlinks)) {
		return []RankedCidNumber{}, 0, nil
	}

	cidLinks := i.backlinks[cidNumber]
	if cidLinks.sortedLinks == nil || len(cidLinks.sortedLinks) == 0 {
		return []RankedCidNumber{}, 0, nil
	}

	totalSize := len(cidLinks.sortedLinks)
	startIndex := page * perPage
	if startIndex >= totalSize {
		return nil, totalSize, errors.New("page not found")
	}

	endIndex := startIndex + perPage
	if endIndex > totalSize {
		endIndex = startIndex + (totalSize % perPage)
	}

	resultSet := cidLinks.sortedLinks[startIndex:endIndex]

	return resultSet, totalSize, nil
}

func (i *BaseSearchIndex) Accounts(account uint64, page, perPage int) (map[link.CidNumber]link.CidNumber, int, error) {

	i.logger.Info("Accounts links query", "account", account, "page", page, "perPage", perPage)

	if i.locked {
		return nil, 0, errors.New("The search index is currently unavailable after node restart")
	}

	//if uint64(cidNumber) >= uint64(len(i.backlinks)) {
	//	return []RankedCidNumber{}, 0, nil
	//}
	//
	//cidLinks := i.backlinks[cidNumber]
	//if cidLinks.sortedLinks == nil || len(cidLinks.sortedLinks) == 0 {
	//	return []RankedCidNumber{}, 0, nil
	//}
	//
	//totalSize := len(cidLinks.sortedLinks)
	//startIndex := page * perPage
	//if startIndex >= totalSize {
	//	return nil, totalSize, errors.New("page not found")
	//}
	//
	//endIndex := startIndex + perPage
	//if endIndex > totalSize {
	//	endIndex = startIndex + (totalSize % perPage)
	//}
	//
	//resultSet := cidLinks.sortedLinks[startIndex:endIndex]
	//
	//return resultSet, totalSize, nil
	totalSize := len(i.accounts[types.AccNumber(account)])

	return i.accounts[types.AccNumber(account)], totalSize, nil
}

func (i *BaseSearchIndex) Top(page, perPage int) ([]RankedCidNumber, int, error) {
	if i.locked {
		return nil, 0, errors.New("The search index is currently unavailable after node restart")
	}

	totalSize := len(i.rank.TopCIDs)
	startIndex := page * perPage
	if startIndex >= totalSize {
		return nil, totalSize, errors.New("page not found")
	}

	endIndex := startIndex + perPage
	if endIndex > totalSize {
		endIndex = startIndex + (totalSize % perPage)
	}

	resultSet := i.rank.TopCIDs[startIndex:endIndex]

	return resultSet, totalSize, nil
}

// make sure that this link (from-to) is new
func (i *BaseSearchIndex) handleLink(link link.CompactLink) {

	i.extendIndex(uint64(link.From()))

	fromIndex := i.links[link.From()]
	// in case unlock signal received we could operate on this index otherwise put link in the end of queue and finish
	select {
	case _ = <-fromIndex.unlockSignal:
		i.putLinkIntoIndex(link.From(), link.To())
		fromIndex.Unlock()
		break
	default:
		i.linksChan <- link
	}
}

func (i *BaseSearchIndex) handleBacklink(link link.CompactLink) {

	i.extendReverseIndex(uint64(link.To()))

	toIndex := i.backlinks[link.To()]
	// in case unlock signal received we could operate on this index otherwise put link in the end of queue and finish
	select {
	case _ = <-toIndex.unlockSignal:
		i.putBacklinkIntoIndex(link.From(), link.To())
		toIndex.Unlock()
		break
	default:
		i.linksChan <- link
	}
}

func (i *BaseSearchIndex) GetRankValue(cid link.CidNumber) uint64 {
	if i.rank.Values == nil || uint64(len(i.rank.Values)) <= uint64(cid) {
		return 0
	}
	return i.rank.Values[cid]
}

func (i *BaseSearchIndex) extendIndex(fromCidNumber uint64) {
	indexLen := uint64(len(i.links))
	if fromCidNumber >= indexLen {
		for j := indexLen; j <= fromCidNumber; j++ {
			links := NewCidLinks()
			links.Unlock() // allow operations on this index
			i.links = append(i.links, links)
		}
	}
}

func (i *BaseSearchIndex) extendReverseIndex(fromCidNumber uint64) {
	indexLen := uint64(len(i.backlinks))
	if fromCidNumber >= indexLen {
		for j := indexLen; j <= fromCidNumber; j++ {
			backlinks := NewCidLinks()
			backlinks.Unlock() // allow operations on this index
			i.backlinks = append(i.backlinks, backlinks)
		}
	}
}

func (i *BaseSearchIndex) putLinkIntoIndex(from link.CidNumber, to link.CidNumber) {
	fromLinks := i.links[uint64(from)].sortedLinks
	rankedTo := RankedCidNumber{to, i.GetRankValue(to)}
	pos := sort.Search(len(fromLinks), func(i int) bool { return fromLinks[i].rank < rankedTo.rank })
	fromLinks = append(fromLinks, RankedCidNumber{})
	copy(fromLinks[pos+1:], fromLinks[pos:])
	fromLinks[pos] = rankedTo
	i.links[uint64(from)].sortedLinks = fromLinks
}

func (i *BaseSearchIndex) putBacklinkIntoIndex(from link.CidNumber, to link.CidNumber) {
	toLinks := i.backlinks[uint64(to)].sortedLinks
	rankedFrom := RankedCidNumber{from, i.GetRankValue(from)}
	pos := sort.Search(len(toLinks), func(i int) bool { return toLinks[i].rank < rankedFrom.rank })
	toLinks = append(toLinks, RankedCidNumber{})
	copy(toLinks[pos+1:], toLinks[pos:])
	toLinks[pos] = rankedFrom
	i.backlinks[uint64(to)].sortedLinks = toLinks
}

// for parallel usage
func (i *BaseSearchIndex) startListenNewLinks() {
	defer func() {
		if r := recover(); r != nil {
			i.errChan <- r.(error)
		}
	}()

	i.logger.Info("The search index is starting to listen to new links")
	for {
		link := <-i.linksChan
		i.handleLink(link)
		i.handleBacklink(link)
	}
}

// for parallel usage
func (i *BaseSearchIndex) startListenNewRank() {
	defer func() {
		if r := recover(); r != nil {
			i.errChan <- r.(error)
		}
	}()

	i.logger.Info("The search index is starting to listen to new rank")
	for {
		rank := <-i.rankChan // TODO could be problems if recalculation lasts more than rank period
		i.rank = rank
		i.recalculateIndices()
	}
}

func (i *BaseSearchIndex) recalculateIndices() {
	defer i.unlock()
	n := len(i.links) // TODO: fix index length to avoid concurrency modification

	// TODO: run in parallel
	for j := 0; j < n; j++ {

		<-i.links[j].unlockSignal // wait till some operations done on this index

		currentSortedLinks := i.links[j].sortedLinks
		newSortedLinks := make(sortableCidNumbers, 0, len(currentSortedLinks))
		for _, cidNumber := range currentSortedLinks {
			newRankedCid := RankedCidNumber{cidNumber.number, i.GetRankValue(cidNumber.number)}
			newSortedLinks = append(newSortedLinks, newRankedCid)
		}
		sort.Stable(sort.Reverse(newSortedLinks))
		i.links[j].sortedLinks = newSortedLinks
		i.links[j].Unlock()
	}

	// same process for backlinks
	n = len(i.backlinks)

	for j := 0; j < n; j++ {

		<-i.backlinks[j].unlockSignal // wait till some operations done on this index

		currentSortedLinks := i.backlinks[j].sortedLinks
		newSortedLinks := make(sortableCidNumbers, 0, len(currentSortedLinks))
		for _, cidNumber := range currentSortedLinks {
			newRankedCid := RankedCidNumber{cidNumber.number, i.GetRankValue(cidNumber.number)}
			newSortedLinks = append(newSortedLinks, newRankedCid)
		}
		sort.Stable(sort.Reverse(newSortedLinks))
		i.backlinks[j].sortedLinks = newSortedLinks
		i.backlinks[j].Unlock()
	}
}

func (i *BaseSearchIndex) lock() {
	i.locked = true
}

func (i *BaseSearchIndex) unlock() {
	i.locked = false
}

func (i *BaseSearchIndex) checkIndexError() error {
	select {
	case err := <-i.errChan:
		return err
	default:
	}
	return nil
}
