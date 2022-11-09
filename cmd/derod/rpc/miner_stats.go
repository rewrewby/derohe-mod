package rpc

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/deroproject/derohe/globals"
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
		globals.Console_Only_Logger.Info(fmt.Sprintf(green+"Height: %d"+reset_color+" - "+green+"%s"+reset_color+": "+green+"Successfully found DERO integrator block\t"+red+"("+blue+"going to submit 🏆"+red+")"+reset_color, chain.Get_Height(), chain.IntegratorAddress().String()))

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

		globals.Console_Only_Logger.Info(fmt.Sprintf(yellow+"Height: %d"+reset_color+" - "+green+"%s"+reset_color+": "+text_color+"Successfully found DERO mini block [%s:9]\t"+red+"("+blue+"going to submit "+emoji+red+")"+reset_color, chain.Get_Height()+1, wallet, argument))

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
