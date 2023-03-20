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

package config

// all global configuration variables are picked from here

// some seed nodes for mainnet (these seed node are not compliant with earlier protocols)
// only version 2
var Mainnet_seed_nodes = []string{
	"89.38.99.117:8443",    // official seed node
	"109.236.81.137:8080",  // official seed node
	"89.38.97.110:11011",   // official seed node
	"190.2.136.120:11011",  // official seed node
	"74.208.54.173:50404",  // (deronfts)
	"85.214.253.170:53387", // (mmarcel-vps)
	"51.222.86.51:11011",   // (RabidMining Pool)
	"163.172.26.245:10505", // (DeroStats)
	"5.161.123.196:11011",  // (MySrvCloud VA)
	"213.171.208.37:18089", // (🔥 MySrvCloud 🔥)
	"44.198.24.170:20000",  // (pieswap)
	"15.235.184.172:11011", // dero-node-sg.mysrv.cloud
	"209.58.186.186:11011", // foundation seed node
	"78.159.118.236:11011", // foundation seed node
	"23.81.165.146:11011",  // foundation seed node
	"85.17.52.28:11011",    // foundation seed node
}

// some seed node for testnet
var Testnet_seed_nodes = []string{
	"212.8.242.60:40401",
}
