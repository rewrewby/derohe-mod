// Copyright 2017-2021 DERO Project. All rights reserved.
// Use of this source code in any form is governed by RESEARCH license.
// license can be found in the LICENSE file.
// GPG: 0F39 E425 8C65 3947 702A  8234 08B2 0360 A03A 9DE8
//
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY
// EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL
// THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
// PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT,
// STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF
// THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package p2p

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/deroproject/derohe/block"

	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/cryptography/crypto"
	"github.com/deroproject/derohe/errormsg"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/metrics"
	"github.com/deroproject/derohe/transaction"
)

var green string = "\033[32m"      // default is green color
var yellow string = "\033[33m"     // make prompt yellow
var red string = "\033[31m"        // make prompt red
var blue string = "\033[34m"       // blue color
var reset_color string = "\033[0m" // reset color
var last_height uint64 = 0

// handles notifications of inventory
func (c *Connection) NotifyINV(request ObjectList, response *Dummy) (err error) {
	defer handle_connection_panic(c)
	var need ObjectList
	var dirty = false

	c.logger.V(3).Info("incoming INV", "request", request)

	if len(request.Block_list) >= 1 { //  handle incoming blocks list
		for i := range request.Block_list { //
			if !chain.Is_Block_Topological_order(request.Block_list[i]) { // block is not in our chain
				if !chain.Block_Exists(request.Block_list[i]) { // check whether the block can be loaded from disk
					need.Block_list = append(need.Block_list, request.Block_list[i])
					dirty = true
				}
			}
		}
	}

	if len(request.Tx_list) >= 1 { // handle incoming tx list and see whether it exists in mempoolor regpool
		for i := range request.Tx_list { //

			// track transaction propagation
			if request.Sent != 0 && request.Sent < globals.Time().UTC().UnixMicro() {
				time_to_receive := float64(globals.Time().UTC().UnixMicro()-request.Sent) / 1000000
				metrics.Set.GetOrCreateHistogram("tx_propagation_duration_histogram_seconds").Update(time_to_receive)
			}
			if !(chain.Mempool.Mempool_TX_Exist(request.Tx_list[i]) || chain.Regpool.Regpool_TX_Exist(request.Tx_list[i])) { // check if is already in mempool skip it
				if _, err = chain.Store.Block_tx_store.ReadTX(request.Tx_list[i]); err != nil { // check whether the tx can be loaded from disk
					need.Tx_list = append(need.Tx_list, request.Tx_list[i])
					dirty = true
				}
			}
		}
	}

	// cchunk list ids are 65 bytes long
	if len(request.Chunk_list) >= 1 { // handle incoming chunks list and see whether we have the chunks
		for i := range request.Chunk_list { //
			var blid, hhash [32]byte
			copy(blid[:], request.Chunk_list[i][:])
			cid := uint8(request.Chunk_list[i][32])
			copy(hhash[:], request.Chunk_list[i][33:])

			// track chunk propagation
			if request.Sent != 0 && request.Sent < globals.Time().UTC().UnixMicro() {
				time_to_receive := float64(globals.Time().UTC().UnixMicro()-request.Sent) / 1000000
				metrics.Set.GetOrCreateHistogram("chunk_propagation_duration_histogram_seconds").Update(time_to_receive)
			}

			if !chain.Block_Exists(blid) { // check whether the block can be loaded from disk
				if nil == is_chunk_exist(hhash, cid) { // if chunk does not exist
					c.logger.V(3).Info("requesting INV chunk", "blid", fmt.Sprintf("%x", blid), "cid", cid, "hhash", fmt.Sprintf("%x", hhash), "raw", fmt.Sprintf("%x", request.Chunk_list[i]))
					need.Chunk_list = append(need.Chunk_list, request.Chunk_list[i])
					dirty = true
				}
			}
		}
	}

	if dirty { //  request inventory only if we want it
		var oresponse Objects
		fill_common(&need.Common) // fill common info

		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()

		if err = c.Client.CallWithContext(ctx, "Peer.GetObject", need, &oresponse); err != nil {
			c.logger = logger.WithName(c.Addr.String())
			c.logger.V(2).Error(err, "Call failed GetObject", "need_objects", need)
			c.exit("Call failed GetObject")
			return
		} else { // process the response
			if err = c.process_object_response(oresponse, request.Sent, false); err != nil {
				return
			}
		}
	}

	c.update(&request.Common)     // update common information
	fill_common(&response.Common) // fill common info

	return nil

}

// only miniblocks carry extra info, which leads to better time tracking
func (c *Connection) NotifyMiniBlock(request Objects, response *Dummy) (err error) {
	defer handle_connection_panic(c)

	// for saving time we got the block
	request_time := globals.Time().UTC().UnixMicro()

	if len(request.MiniBlocks) >= 5 {
		err = fmt.Errorf("Notify Block can notify max 5 miniblocks")
		c.logger.V(3).Error(err, "Should be banned")
		c.exit("Should be banned")
		return err
	}
	fill_common_T1(&request.Common)
	c.update(&request.Common) // update common information

	if c.ActiveTrace {
		c.logger.Info("Incoming NotifyMiniBlock Request", "request", request)
	}

	var mbls []block.MiniBlock

	height := uint64(chain.Get_Height())
	for i := range request.MiniBlocks {
		var mbl block.MiniBlock
		if err = mbl.Deserialize(request.MiniBlocks[i]); err != nil {
			return err
		}

		// MiniBlock Spam Prevention
		if mbl.Height >= height-1 {
			mbls = append(mbls, mbl)
		} else {
			return fmt.Errorf("Stale Miniblock")
		}

		atomic.AddUint64(&c.BytesIn, 1)
	}

	for _, mbl := range mbls {
		var ok bool

		if mbl.Final {
			return fmt.Errorf("final blocks are not propagted")
		}

		// track miniblock propagation
		if request.Sent != 0 && request.Sent < globals.Time().UTC().UnixMicro() {
			time_to_receive := float64(globals.Time().UTC().UnixMicro()-request.Sent) / 1000000
			metrics.Set.GetOrCreateHistogram("miniblock_propagation_duration_histogram_seconds").Update(time_to_receive)
		}

		go LogMiniblock(mbl, c.Addr.String(), request_time)

		// first check whether it is already in the chain
		if chain.MiniBlocks.IsCollision(mbl) {
			continue // miniblock already in chain, so skip it
		}

		var miner_hash crypto.Hash
		copy(miner_hash[:], mbl.KeyHash[:])
		if !chain.IsAddressHashValid(false, miner_hash) { // this will use cache
			return fmt.Errorf("unregistered miner")
		}

		var bl block.Block

		bl.Height = mbl.Height
		var tip1, tip2 crypto.Hash
		binary.BigEndian.PutUint32(tip1[:], mbl.Past[0])
		bl.Tips = append(bl.Tips, tip1)

		if mbl.PastCount == 2 {
			binary.BigEndian.PutUint32(tip2[:], mbl.Past[1])
			bl.Tips = append(bl.Tips, tip2)
		}

		for i, tip := range bl.Tips { // tips are currently only partial,  lets expand tips
			if ehash, ok := chain.ExpandMiniBlockTip(tip); ok {
				bl.Tips[i] = ehash
			} else {
				return fmt.Errorf("tip could not be expanded")
			}
		}

		bl.MiniBlocks = append(bl.MiniBlocks, mbl)

		// lets get the difficulty at tips
		if !chain.VerifyMiniblockPoW(&bl, mbl) {
			return errormsg.ErrInvalidPoW
		}

		if err, ok = chain.InsertMiniBlock(mbl); !ok {
			if c.ActiveTrace {
				c.logger.Info("Bad MiniBlock in NotifyMiniBlock Request", "err", err.Error())
			}
			go PeerLogReceiveFail(c.Addr.String(), "InsertMiniBlock", c.Peer_ID, err.Error())
			// this happens all the time?
			go LogReject(c.Addr.String())
			return err
		} else { // rebroadcast miniblock

			chain.MiniBlocks.RLock()
			globals.MiniBlocksCollectionCount = uint8(len(chain.MiniBlocks.Collection[mbl.GetKey()]))
			chain.MiniBlocks.RUnlock()

			atomic.AddInt64(&globals.CountTotalBlocks, 1)
			wallet := GetMinerAddressFromKeyHash(chain, mbl)

			go CheckIfMiniBlockIsOrphaned(false, mbl, wallet)

			if config.RunningConfig.TraceBlocks {

				text_color := green
				if globals.MiniBlocksCollectionCount > 9 {
					text_color = yellow
				}

				wallet_color := blue

				// check if wallet is local and make green
				globals.Console_Only_Logger.Info(fmt.Sprintf(green+"Height: "+yellow+"%d"+reset_color+" - "+wallet_color+"%s"+reset_color+": "+text_color+"Successfully found DERO mini block [%d:9]", mbl.Height, wallet, globals.MiniBlocksCollectionCount))
			}

			broadcast_MiniBlock(mbl, c.Peer_ID, request.Sent) // do not send back to the original peer

			if c.ActiveTrace {
				c.logger.Info("Good MiniBlock in NotifyMiniBlock - Broadcasting")
			}

			globals.ForeignMiniFoundTime_lock.Lock()
			defer globals.ForeignMiniFoundTime_lock.Unlock()
			globals.ForeignMiniFoundTime[wallet] = append(globals.ForeignMiniFoundTime[wallet], time.Now().Unix())

			go LogAccept(c.Addr.String())

		}
	}
	if c.ActiveTrace {
		c.logger.Info("Incoming NotifyMiniBlock Request", "response", response)
	}
	fill_common(&response.Common)                         // fill common info
	fill_common_T0T1T2(&request.Common, &response.Common) // fill time related information
	return nil
}

func (c *Connection) processChunkedBlock(request Objects, data_shard_count, parity_shard_count int) error {
	var err error

	var cbl block.Complete_Block // parse incoming block and deserialize it
	var bl block.Block
	// lets deserialize block first and see whether it is the requested object
	cbl.Bl = &bl
	err = bl.Deserialize(request.CBlocks[0].Block)
	if err != nil { // we have a block which could not be deserialized ban peer
		c.logger.V(3).Error(err, "Block cannot be deserialized.Should be banned")
		c.exit("Block cannot be deserialized.Should be banned")
		return err
	}

	blid := bl.GetHash()
	atomic.AddUint64(&c.BytesIn, 1)

	// object is already is in our chain, we need not relay it
	if chain.Is_Block_Topological_order(blid) || chain.Is_Block_Tip(blid) {
		return nil
	}

	// the block is not in our db,  parse entire block, complete the txs and try to add it
	if len(bl.Tx_hashes) == len(request.CBlocks[0].Txs) {
		c.logger.V(2).Info("Received a complete block", "blid", blid, "txcount", len(bl.Tx_hashes))
		for j := range request.CBlocks[0].Txs {
			var tx transaction.Transaction
			err = tx.Deserialize(request.CBlocks[0].Txs[j])
			if err != nil { // we have a tx which could not be deserialized ban peer
				c.logger.Error(err, "tx cannot be deserialized.Should be banned")
				c.exit("tx cannot be deserialized.Should be banned")
				return err
			}
			cbl.Txs = append(cbl.Txs, &tx)
		}
	} else { // the block is NOT complete, we consider it as an ultra compact block
		c.logger.V(2).Info("Received an ultra compact  block", "blid", blid, "txcount", len(bl.Tx_hashes), "skipped", len(bl.Tx_hashes)-len(request.CBlocks[0].Txs), "stacktrace", globals.StackTrace(false))
		// how
	}

	// make sure connection does not timeout and be killed while processing huge blocks
	atomic.StoreInt64(&c.LastObjectRequestTime, time.Now().Unix())
	// check if we can add ourselves to chain
	if err, ok := chain.Add_Complete_Block(&cbl); ok { // if block addition was successfil
		// notify all peers
		Broadcast_Block(&cbl, c.Peer_ID) // do not send back to the original peer

		go LogFinalBlock(bl, c.Addr.String(), globals.Time().UTC().UnixMicro())
		go LogAccept(c.Addr.String())

		wallet := GetIntegratorAddressFromKeyHash(chain, bl)

		go CheckIfBlockIsOrphaned(false, bl.MiniBlocks[9], wallet)

		if config.RunningConfig.TraceBlocks {

			text_color := green
			if last_height == bl.Height {
				// Display orphans as yellow
				text_color = yellow
			}
			atomic.AddInt64(&globals.CountTotalBlocks, 1)

			last_height = bl.Height

			globals.Console_Only_Logger.Info(fmt.Sprintf(text_color+"Height: %d"+reset_color+" - "+blue+"%s"+reset_color+": "+text_color+"Successfully found DERO integrator block", last_height, wallet))

		}

	} else { // ban the peer for sometime
		c.logger.Error(err, "Error adding block from peer...")
		go PeerLogReceiveFail(c.Addr.String(), "Add_Complete_Block", c.Peer_ID, err.Error())
		go LogReject(c.Addr.String())
		if err == errormsg.ErrInvalidPoW {
			c.logger.Error(err, "This peer should be banned and terminated")
			c.exit("This peer should be banned and terminated")
			return err
		}
	}

	return nil
}
