package p2p

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/deroproject/derohe/block"
	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/globals"
)

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
				go AddBlockToMyOrphanMiniBlockCollection(mblData, miner)
			} else {
				go AddBlockToOrphanMiniBlockCollection(mblData, miner)
			}

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
				go AddBlockToMyOrphanBlockCollection(blockData, miner)
			}
			go AddBlockToOrphanBlockCollection(blockData, miner)

			return
		}
	}
}
