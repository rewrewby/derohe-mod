package rpc

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"bytes"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"sync/atomic"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/deroproject/derohe/blockchain"
	"github.com/deroproject/derohe/cmd/derod/rpc/payment"
	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/graviton"
	"github.com/go-logr/logr"
	"github.com/lesismal/llib/std/crypto/tls"
	"github.com/lesismal/nbio"
	"github.com/lesismal/nbio/logging"
	"github.com/lesismal/nbio/nbhttp"
	"github.com/lesismal/nbio/nbhttp/websocket"
)

type user_pool_session struct {
	shares        uint64
	share_value   uint64
	share_tracker uint64
	lasterr       string
	address       rpc.Address
	valid_address bool
	address_sum   [32]byte
	difficulty    uint64
	custom_diff   uint64
	result_time   time.Time
	sync.RWMutex
}

type pool_session struct {
	miniblocks uint64
	blocks     uint64
	orphaned   uint64
	rejected   uint64
	new_round  bool
	sync.RWMutex
}

type miner_distribution struct {
	value uint64
}

var pool_client_list_mutex sync.Mutex
var lock sync.Mutex
var pool_client_list = map[*websocket.Conn]*user_pool_session{}
var active_miners = map[string]*miner_distribution{}

var pool_stats pool_session
var pool_miners_count int
var shares_sum uint64
var result_counter int
var round_height uint64
var payment_lock bool

var svr_pool *nbhttp.Server
var logger_getwork_pool logr.Logger
var miner_address_hashed_key [32]byte

func CountPoolMiners() int {
	defer cleanup()
	pool_client_list_mutex.Lock()
	defer pool_client_list_mutex.Unlock()

	pool_miners_count = len(pool_client_list)
	return pool_miners_count
}

func pool_SendJob() {

	defer globals.Recover(1)

	// get a block template, and then we will fill the address here as optimization
	bl, mbl_main, _, _, err := chain.Create_new_block_template_mining(chain.IntegratorAddress())
	if err != nil {
		return
	}

	prev_hash := ""
	for i := range bl.Tips {
		prev_hash = prev_hash + bl.Tips[i].String()
	}

	diff := chain.Get_Difficulty_At_Tips(bl.Tips)

	if mbl_main.HighDiff {
		diff.Mul(diff, new(big.Int).SetUint64(config.MINIBLOCK_HIGHDIFF))
	}

	pool_client_list_mutex.Lock()
	defer pool_client_list_mutex.Unlock()

	pool_stats.Lock()
	defer pool_stats.Unlock()

	for rk, rv := range pool_client_list {

		// Set miner difficulty
		rv.difficulty = diff.Uint64()
		if rv.shares > 0 && rv.share_tracker > 0 {
			adjust_miner_difficulty(rv)
		}

		go func(k *websocket.Conn, v *user_pool_session) {
			defer globals.Recover(2)
			var buf bytes.Buffer
			encoder := json.NewEncoder(&buf)

			var params rpc.GetBlockTemplate_Result
			params.JobID = fmt.Sprintf("%d.%d.%s", bl.Timestamp, 0, "notified")
			params.Height = bl.Height
			params.Prev_Hash = prev_hash
			params.Difficultyuint64 = v.custom_diff
			params.Difficulty = fmt.Sprintf("%d", v.custom_diff)
			params.Hansen33Mod = true

			mbl := mbl_main

			if !mbl.Final { //write miners address only if possible
				copy(mbl.KeyHash[:], miner_address_hashed_key[:])
			}

			for i := range mbl.Nonce { // give each user different work
				mbl.Nonce[i] = globals.Global_Random.Uint32() // fill with randomness
			}

			if !v.valid_address && !chain.IsAddressHashValid(false, v.address_sum) {
				params.LastError = "unregistered miner or you need to wait 15 mins"
			} else {
				v.valid_address = true
			}
			// We are sending pool stats to all miners
			// TODO: Send stats since joining the pool
			params.Blockhashing_blob = fmt.Sprintf("%x", mbl.Serialize())
			params.Blocks = pool_stats.blocks
			params.MiniBlocks = pool_stats.miniblocks
			params.Orphans = pool_stats.orphaned
			params.Rejected = pool_stats.rejected

			encoder.Encode(params)
			k.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
			k.WriteMessage(websocket.TextMessage, buf.Bytes())
			buf.Reset()

		}(rk, rv)

	}
}

func pool_newUpgrader() *websocket.Upgrader {
	u := websocket.NewUpgrader()

	u.OnMessage(func(c *websocket.Conn, messageType websocket.MessageType, data []byte) {

		if messageType != websocket.TextMessage {
			return
		}

		sess := c.Session().(*user_pool_session)
		miner := ParseIPNoError(c.RemoteAddr().String())

		pool_client_list_mutex.Lock()
		defer pool_client_list_mutex.Unlock()

		var x rpc.MinerInfo_Params
		if json.Unmarshal(data, &x); len(x.Wallet_Address) > 0 {
			return
		}

		var p rpc.SubmitBlock_Params

		if err := json.Unmarshal(data, &p); err != nil {
			// don't think this ever happens
			logger.Info(fmt.Sprintf("Error: %s", err.Error()))
		}

		mbl_block_data_bytes, err := hex.DecodeString(p.MiniBlockhashing_blob)
		if err != nil {
			//logger.Info("Submitting block could not be decoded")
			sess.lasterr = fmt.Sprintf("Submitted block could not be decoded. err: %s", err)
			return
		}

		var tstamp, extra uint64
		fmt.Sscanf(p.JobID, "%d.%d", &tstamp, &extra)

		pow_hash := astrobwtv3.AstroBWTv3(mbl_block_data_bytes)

		pool_stats.RLock()
		defer pool_stats.RUnlock()

		// -> If miner difficulty matches network difficulty
		// TODO: check if miner is cheating

		if blockchain.CheckPowHash(pow_hash, sess.difficulty) {
			_, blid, sresult, err := chain.Accept_new_block(tstamp, mbl_block_data_bytes)

			if sresult {
				sess.shares++
				if !pool_stats.new_round {
					pool_stats.new_round = true
					round_height = uint64(chain.Get_Height() + 1)
				}
				if sess.result_time.IsZero() {
					sess.result_time = time.Now()
				}
				sess.share_value += sess.difficulty
				sess.share_tracker++

				if blid.IsZero() {
					pool_stats.miniblocks++
					atomic.AddInt64(&CountMinisAccepted, 1)

					rate_lock.Lock()
					defer rate_lock.Unlock()

					// Reset fail count in case of valid PoW
					for i, t := range ban_list {
						if miner == i {
							t.fail_count = 0
							ban_list[miner] = t
						}
					}
				} else {
					pool_stats.blocks++
					atomic.AddInt64(&CountBlocks, 1)
				}
				result_counter++
				set_pool_stats(sess.address.String(), sess.share_value, sess.difficulty)
			}

			if !sresult || err != nil {
				pool_stats.rejected++
				atomic.AddInt64(&CountMinisRejected, 1)

				if err != nil {
					sess.lasterr = err.Error()
				}

				rate_lock.Lock()
				defer rate_lock.Unlock()

				// Increase fail count and ban miner in case of 3 invalid PoW's in a row
				i := ban_list[miner]
				i.fail_count++
				if i.fail_count >= 3 {
					i.timestamp = time.Now()
					c.Close()
					delete(pool_client_list, c)
					logger_getwork.V(1).Info("Banned miner", "Address", miner, "Info", "Banned")
				}
				ban_list[miner] = i
			}
		} else {
			if blockchain.CheckPowHash(pow_hash, sess.custom_diff) {
				sess.shares++
				if !pool_stats.new_round {
					pool_stats.new_round = true
					round_height = uint64(chain.Get_Height() + 1)
				}
				if sess.result_time.IsZero() {
					sess.result_time = time.Now()
				}
				sess.share_value += sess.custom_diff
				sess.share_tracker++
				// Reset fail count in case of valid PoW
				for i, t := range ban_list {
					if miner == i {
						t.fail_count = 0
						ban_list[miner] = t
					}
				}
				set_pool_stats(sess.address.String(), sess.share_value, sess.custom_diff)
			} else {
				// Increase fail count and ban miner in case of 3 invalid PoW's in a row
				i := ban_list[miner]
				i.fail_count++
				if i.fail_count >= 3 {
					i.timestamp = time.Now()
					c.Close()
					delete(pool_client_list, c)
					logger_getwork.V(1).Info("Pool: Banned miner", "Address", miner, "Info", "Banned")
				}
				ban_list[miner] = i
			}
		}
		if chain.Get_Height()-int64(round_height) == int64(config.Payment.PayoutInterval)-1 && !payment_lock {
			go prepare_payout()
		}
	})
	u.OnClose(func(c *websocket.Conn, err error) {
		pool_client_list_mutex.Lock()
		defer pool_client_list_mutex.Unlock()
		delete(pool_client_list, c)

	})

	return u
}

func pool_onWebsocket(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/ws/") {
		http.NotFound(w, r)
		return
	}
	address := strings.TrimPrefix(r.URL.Path, "/ws/")

	addr, err := globals.ParseValidateAddress(address)
	if err != nil {
		fmt.Fprintf(w, "err: %s\n", err)
		return
	}
	addr_raw := addr.PublicKey.EncodeCompressed()

	upgrader := pool_newUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		//panic(err)
		return
	}

	// Check incoming connections if ban still exists
	// Ban is active for 15 minutes
	miner := ParseIPNoError(r.RemoteAddr)
	rate_lock.Lock()

	for i, t := range ban_list {
		if miner == i {
			if time.Now().Sub(t.timestamp) < time.Minute*15 {
				logger_getwork_pool.V(1).Info("Banned miner", "Address", i, "Info", "Ban still active")
				conn.Close()
				break
			} else {
				delete(ban_list, i)
				break
			}
		}
	}
	rate_lock.Unlock()

	wsConn := conn.(*websocket.Conn)

	// Start with difficulty 10,000
	session := user_pool_session{address: *addr, address_sum: graviton.Sum(addr_raw), custom_diff: 5000}
	wsConn.SetSession(&session)

	pool_client_list_mutex.Lock()
	defer pool_client_list_mutex.Unlock()
	pool_client_list[wsConn] = &session
}

func Getwork_pool_server() {

	var err error

	logger_getwork_pool = globals.Logger.WithName("POOL")

	logging.SetLevel(logging.LevelNone) //LevelDebug)//LevelNone)

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{generate_random_tls_cert()},
		InsecureSkipVerify: true,
	}

	mux_pool := &http.ServeMux{}
	mux_pool.HandleFunc("/", pool_onWebsocket) // handle everything

	default_address := fmt.Sprintf("0.0.0.0:%d", globals.Config.POOL_Default_Port)

	if _, ok := globals.Arguments["--pool-bind"]; ok && globals.Arguments["--pool-bind"] != nil {
		addr, err := net.ResolveTCPAddr("tcp", globals.Arguments["--pool-bind"].(string))
		if err != nil {
			logger_getwork_pool.Error(err, "--pool-bind address is invalid")
			return
		} else {
			if addr.Port == 0 {
				logger_getwork_pool.Info("POOL server is disabled, No ports will be opened for miners to get work")
				return
			} else {
				default_address = addr.String()
			}
		}
	}

	logger_getwork_pool.Info("POOL will listen", "address", default_address)

	miner_address_hashed_key = graviton.Sum(chain.IntegratorAddress().Compressed())

	svr_pool = nbhttp.NewServer(nbhttp.Config{
		Name:                    "POOL",
		Network:                 "tcp",
		AddrsTLS:                []string{default_address},
		TLSConfig:               tlsConfig,
		Handler:                 mux_pool,
		MaxLoad:                 10 * 1024,
		MaxWriteBufferSize:      32 * 1024,
		ReleaseWebsocketPayload: true,
		KeepaliveTime:           240 * time.Hour, // we expects all miners to find a block every 10 days,
		NPoller:                 runtime.NumCPU(),
	})

	svr_pool.OnReadBufferAlloc(func(c *nbio.Conn) []byte {
		return memPool.Get().([]byte)
	})
	svr_pool.OnReadBufferFree(func(c *nbio.Conn, b []byte) {
		memPool.Put(b)
	})

	//globals.Cron.AddFunc("@every 2s", SendJob) // if daemon restart automaticaly send job
	go func() { // try to be as optimized as possible to lower hash wastage
		if config.RunningConfig.GETWorkJobDispatchTime.Milliseconds() < 40 {
			config.RunningConfig.GETWorkJobDispatchTime = 500 * time.Millisecond
		}
		logger_getwork_pool.Info("Job will be dispatched every", "time", config.RunningConfig.GETWorkJobDispatchTime)
		old_mini_count := 0
		old_time := time.Now()
		old_height := int64(0)
		for {
			if pool_miners_count > 0 {
				current_mini_count := chain.MiniBlocks.Count()
				current_height := chain.Get_Height()
				if old_mini_count != current_mini_count || old_height != current_height || time.Now().Sub(old_time) > config.RunningConfig.GETWorkJobDispatchTime {
					old_mini_count = current_mini_count
					old_height = current_height
					pool_SendJob()
					old_time = time.Now()
				}
			} else {

			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	logger.Info("Waiting 5 second to start getwork pool")
	time.Sleep(5 * time.Second)

	if err = svr_pool.Start(); err != nil {
		logger_getwork_pool.Error(err, "nbio.Start failed.")
		return
	}
	logger.Info("POOL/Websocket server started")
	svr_pool.Wait()
	defer svr_pool.Stop()

}

// Basic difficulty adjustment
// TODO: Implement very cool algorithm
func adjust_miner_difficulty(session *user_pool_session) {

	var zerotime time.Time
	var ratio float32

	session.RLock()
	defer session.RUnlock()

	if time.Now().Sub(session.result_time) >= time.Second*180 {
		ratio = float32(session.share_tracker) / 10
		if ratio < 1 && session.custom_diff > 1000 {
			session.custom_diff -= 1000
		}
		if ratio > 2 {
			session.custom_diff += 1000
		}
		session.share_tracker = 0
		session.result_time = zerotime
	}

}

// save stats to disk
func save_pool_stats() {
	var store bytes.Buffer
	var str_buf string

	store.WriteString(fmt.Sprintf("Miner distribution from block %d to %d\n", round_height, round_height+(config.Payment.PayoutInterval-1)))

	for m, v := range active_miners {
		str_buf = m + ":" + fmt.Sprintf("%d\n", v.value)
		store.WriteString(str_buf)
	}
	str_buf = fmt.Sprintf("%d", shares_sum)
	store.WriteString(str_buf)

	fd, _ := os.OpenFile("pool_active_miners.txt", os.O_CREATE, 0666)
	fd.Write(store.Bytes())
	fd.Close()
}

// update miner/pool stats
func set_pool_stats(address string, value uint64, count uint64) {
	lock.Lock()
	defer lock.Unlock()

	new := miner_distribution{value: value}
	active_miners[address] = &new
	shares_sum += count

	save_pool_stats()
}

func reset_pool_stats() {

	for m := range active_miners {
		delete(active_miners, m)
	}

	for m, v := range pool_client_list {
		v.share_value = 0
		pool_client_list[m] = v
	}

	shares_sum = 0
	round_height = 0
	pool_stats.new_round = false

	return
}

func prepare_payout() {

	payment_lock = true
	// Give some time to settle
	time.Sleep(time.Second * 5)

	lock.Lock()
	defer lock.Unlock()

	type miner_tx struct {
		address string
		amount  uint64
	}

	var miner_total uint64
	var rounds int
	var i int

	count, total := payment.Get_coinbase_rewards(round_height)
	// skip in case of no rewards
	if total == 0 || count == 0 {
		logger_getwork_pool.V(0).Info("Payout: No coinbase rewards. Check payment wallet!")
		return
	}

	active_miners_count := len(active_miners)
	txn := make([]miner_tx, active_miners_count)

	rounds = (active_miners_count / 64) + 1

	for m, v := range active_miners {
		if m == chain.IntegratorAddress().String() {
			continue
		}
		base := float64(v.value) / float64(shares_sum)
		sum_float := math.Floor(float64(total) * base)
		//TODO pool op fees
		miner_total = uint64(sum_float) - 161
		txn[i].address = m
		txn[i].amount = miner_total
		i++
	}

	var counter int
	var tx_params rpc.Transfer_Params
	var tx rpc.Transfer

	tx_params.Ringsize = uint64(config.Payment.Ringsize)
	for i = 0; i < rounds; i++ {
		for j := 0; j < config.Payment.TxsPerPayout; j++ {
			if j == active_miners_count {
				break
			}
			tx.Destination = txn[counter].address
			tx.Amount = txn[counter].amount
			tx_params.Transfers = append(tx_params.Transfers, tx)
			counter++
		}
		txid, err := payment.Payout(&tx_params)
		if txid != "" {
			logger_getwork_pool.V(0).Info("Payout succeeded", "TXID", txid)
		} else {
			logger_getwork_pool.V(0).Error(err, "Payout error. Can't send transaction. Check payment wallet!")
		}
		tx_params.Transfers = nil
	}

	reset_pool_stats()
	payment_lock = false
}
