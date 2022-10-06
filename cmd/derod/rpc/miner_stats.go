package rpc

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/deroproject/derohe/block"
	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/p2p"
)

type inner_miner_stats struct {
	blocks     uint64
	miniblocks uint64
	rejected   uint64
	orphaned   uint64
	// hashrate   float64
	lasterr     string
	miners      int64
	feesoverdue bool
	feeaddress  string
}

var miner_stats_mutex sync.Mutex
var miner_stats = make(map[string]inner_miner_stats)

func MinerMetric(ip string, wallet string, counter string, argument string) {

	logger := globals.Logger.WithName("RPC")

	miner_stats_mutex.Lock()
	defer miner_stats_mutex.Unlock()

	i := miner_stats[wallet]

	switch counter {

	case "newminer":

		i.miners++

	case "byeminer":
		i.miners--

	case "blocks":
		i.blocks++
		logger.Info(fmt.Sprintf(green+"Height: %d"+reset_color+" - "+green+"%s"+reset_color+": "+green+"Successfully found DERO integrator block\t"+red+"("+blue+"going to submit 🏆"+red+")"+reset_color, chain.Get_Height(), wallet))

	case "miniblocks":
		i.miniblocks++

		emoji := "🏆"
		text_color := green
		if s, err := strconv.ParseInt(argument, 10, 64); err == nil {
			if s > 9 {
				emoji = "😅"
				text_color = yellow
			}
		}

		logger.Info(fmt.Sprintf(yellow+"Height: %d"+reset_color+" - "+green+"%s"+reset_color+": "+text_color+"Successfully found DERO mini block [%s:9]\t"+red+"("+blue+"going to submit "+emoji+red+")"+reset_color, chain.Get_Height()+1, wallet, argument))

	case "rejected":
		i.rejected++
	case "orphaned":
		i.orphaned++

	case "lasterror":
		i.lasterr = argument

	case "feeisdue":
		i.feesoverdue = true
		i.feeaddress = argument
		logger.Info(fmt.Sprintf("Fee is due for %s", wallet))
		i.lasterr = "! Cheater !"

	case "feeispaid":
		i.feesoverdue = false
		i.blocks++
		i.lasterr = argument

	default:

	}

	miner_stats[wallet] = i

	// count unique miners
	count := 0
	for _, stat := range miner_stats {
		if stat.miners >= 1 {
			count++
		}
	}
	globals.CountUniqueMiners = int64(count)

}

func GetAllMinerStats() map[string]inner_miner_stats {

	miner_stats_mutex.Lock()
	defer miner_stats_mutex.Unlock()

	var stats = make(map[string]inner_miner_stats)

	// copy hash
	for key, value := range miner_stats {
		x := stats[key]
		x = value
		stats[key] = x
	}

	return stats
}

func getMinerStats(wallet string) (stats inner_miner_stats) {

	miner_stats_mutex.Lock()
	defer miner_stats_mutex.Unlock()

	stats = miner_stats[wallet]

	return stats
}

func CheckIfMiniBlockIsOrphaned(local bool, mblData block.MiniBlock, miner string) {

	var fails int

	for {
		time.Sleep(time.Second)
		if chain.Get_Height() >= int64(mblData.Height)+1 {
			// logger.V(2).Info(fmt.Sprintf("Height: %d - Checking if orphaned (%s)", mblData.Height, mblData.GetHash().String()))
			block, err := chain.Load_Block_Topological_order_at_index(int64(mblData.Height))
			if err != nil {
				fails++
				if fails == 5 {
					return
				}
				continue
			}
			bl, err := chain.Load_BL_FROM_ID(block)
			if err != nil {
				fails++
				if fails == 5 {
					return
				}
				continue
			}
			for _, mbl := range bl.MiniBlocks {
				if mbl == mblData {
					return
				}
			}

			// Mini Block is Orphan

			logger.V(2).Info(fmt.Sprintf("Height: %d - %s Miniblock (%s) ORPHANED", mblData.Height, miner, mblData.GetHash().String()))

			if local {
				logger.Info(fmt.Sprintf(red+"Height: %d"+reset_color+" - "+red+"%s"+reset_color+": "+blue+"Orphan DERO mini block\t"+yellow+"("+red+"Profit Loss 💣"+yellow+")"+reset_color+reset_color, mblData.Height, miner))
			} else {
				if config.RunningConfig.TraceBlocks {
					logger.Info(fmt.Sprintf(red+"Height: %d"+reset_color+" - "+red+"%s"+reset_color+": "+blue+"Orphan DERO mini block"+reset_color, mblData.Height, miner))
				}
			}

			if local {
				atomic.AddInt64(&globals.CountOrphanMinis, 1)
				go p2p.AddBlockToMyOrphanMiniBlockCollection(mblData, miner)
			}
			go p2p.AddBlockToOrphanMiniBlockCollection(mblData, miner)

			return
		}
	}
}

func CheckIfBlockIsOrphaned(local bool, blockData block.MiniBlock, miner string) {

	var fails int

	for {
		time.Sleep(time.Second)
		if chain.Get_Height() >= int64(blockData.Height)+1 {
			// logger.V(2).Info(fmt.Sprintf("Height: %d - Checking if orphaned (%s)", mblData.Height, mblData.GetHash().String()))
			block_id, err := chain.Load_Block_Topological_order_at_index(int64(blockData.Height))
			if err != nil {
				fails++
				if fails == 5 {
					return
				}
				continue
			}

			bl, err := chain.Load_BL_FROM_ID(block_id)
			if err != nil {
				logger.Error(err, "loading block", "blid", block_id, "height", blockData.Height)
			}

			// Is not orphan
			if bl.MiniBlocks[9] == blockData {
				// logger.V(2).Info(fmt.Sprintf("Height: %d - %s Integrator block (%s) NOT ORPHANED", blockData.Height, miner, blockData.GetHash().String()))
				return
			}

			logger.V(2).Info(fmt.Sprintf("Height: %d - %s Integrator block (%s) ORPHANED", blockData.Height, miner, blockData.GetHash().String()))

			if local {
				logger.Info(fmt.Sprintf(red+"Height: %d"+reset_color+" - "+red+"%s"+reset_color+": "+blue+"Orphan DERO integrator block\t"+yellow+"("+red+"Profit Loss 💣"+yellow+")"+reset_color+reset_color, blockData.Height, miner))
			} else {
				if config.RunningConfig.TraceBlocks {
					logger.Info(fmt.Sprintf(red+"Height: %d"+reset_color+" - "+red+"%s"+reset_color+": "+blue+"Orphan DERO integrator block"+reset_color, blockData.Height, miner))
				}
			}

			if local {
				atomic.AddInt64(&globals.CountOrphanBlocks, 1)
				// Update Miner
				go p2p.AddBlockToMyOrphanBlockCollection(blockData, miner)
			}
			go p2p.AddBlockToOrphanBlockCollection(blockData, miner)

			return
		}
	}
}
