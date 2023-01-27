package p2p

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/globals"
)

var trusted_map = make(map[string]int64)
var trust_mutex sync.Mutex

func GetTrustedMap() map[string]int64 {

	trust_mutex.Lock()
	defer trust_mutex.Unlock()

	var mymap = make(map[string]int64)

	for key, value := range trusted_map {
		mymap[key] = value
	}

	return mymap

}

func IsTrustedIP(Addr string) bool {

	trust_mutex.Lock()
	defer trust_mutex.Unlock()

	Address := ParseIPNoError(Addr)

	for _, ip := range config.Mainnet_seed_nodes {
		if Address == ParseIPNoError(ip) {
			return true
		}
	}

	for ip, _ := range trusted_map {
		if Address == ParseIPNoError(ip) {
			if Addr != ip {

				_, _, err := net.SplitHostPort(Addr)
				if err == nil {
					logger.V(2).Info(fmt.Sprintf("Updating trusted map %s -> %s", ip, Addr))
					trusted_map[Addr] = trusted_map[ip]
					delete(trusted_map, ip)
					// go save_trust_list()
				}

			}
			return true
		}
	}

	// logger.V(1).Info(fmt.Sprintf("%s is not a trusted node", Address))

	return false
}

// loads peers list from disk
func LoadTrustedList() {

	peer_mutex.Lock()
	defer peer_mutex.Unlock()

	trust_mutex.Lock()
	defer trust_mutex.Unlock()

	peer_file := filepath.Join(globals.GetDataDirectory(), "trusted_peers.json")
	if _, err := os.Stat(peer_file); errors.Is(err, os.ErrNotExist) {
		return // since file doesn't exist , we cannot load it
	}
	file, err := os.Open(peer_file)
	if err != nil {
		logger.Error(err, "opening peer file")
	} else {
		defer file.Close()
		decoder := json.NewDecoder(file)
		err = decoder.Decode(&trusted_map)
		if err != nil {
			logger.Error(err, "Error unmarshalling peer data")
		} else { // successfully unmarshalled data
			logger.V(1).Info("Successfully loaded peers from file", "peer_count", (len(trusted_map)))
		}
	}

}

// save peer list to disk
func SaveTrustList() {

	trust_mutex.Lock()
	defer trust_mutex.Unlock()

	peer_file := filepath.Join(globals.GetDataDirectory(), "trusted_peers.json")
	file, err := os.Create(peer_file)
	if err != nil {
		logger.Error(err, "saving peer file")
	} else {
		defer file.Close()
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "\t")
		err = encoder.Encode(&trusted_map)
		if err != nil {
			logger.Error(err, "Error marshalling peer data")
		} else { // successfully unmarshalled data
			logger.V(1).Info("Successfully saved peers to file", "peer_count", (len(trusted_map)))
		}
	}
}

func Add_Trusted(Addr string) {
	trust_mutex.Lock()
	defer trust_mutex.Unlock()

	Address := ParseIPNoError(Addr)

	if len(Address) < 1 {
		return
	}

	found := false
	for _, c := range UniqueConnections() {

		ip := ParseIPNoError(c.Addr.String())

		if ip == ParseIPNoError(Address) {
			trusted_map[c.Addr.String()] = int64(time.Now().UTC().Unix())
			logger.Info(fmt.Sprintf("Address: %s (%s) - Added to Trusted List", ip, c.Tag))
			found = true
			break
		}

		tag_match := regexp.MustCompile(Address)
		if tag_match.Match([]byte(c.Tag)) {
			trusted_map[c.Addr.String()] = int64(time.Now().UTC().Unix())
			logger.Info(fmt.Sprintf("Address: %s (%s) - Added to Trusted List", ip, c.Tag))
			found = true
			break
		}
	}

	if !found {
		trusted_map[Addr] = int64(time.Now().UTC().Unix())
	}

	// Mainnet_seed_nodes - add to this to maintain connections

	go SaveTrustList()

}

func Del_Trusted(Address string) {
	trust_mutex.Lock()
	defer trust_mutex.Unlock()

	for ip, _ := range trusted_map {
		if ip == ParseIPNoError(Address) {
			delete(trusted_map, ParseIPNoError(ip))
			logger.Info(fmt.Sprintf("Address: %s - Removed from Trusted List", ip))
		}
	}

	for _, c := range UniqueConnections() {
		tag_match := regexp.MustCompile(Address)
		if tag_match.Match([]byte(c.Tag)) {
			delete(trusted_map, ParseIPNoError(c.Addr.String()))
			logger.Info(fmt.Sprintf("Address: %s (%s) - Removed from Trusted List", c.Addr.String(), c.Tag))
		}
	}

	go SaveTrustList()
}

func Print_Trusted_Peers() {

	unique_map := UniqueConnections()
	fmt.Printf("Trusted Peers\n\n")

	fmt.Printf("Seed Nodes (Always Trusted)\n")
	for _, ip := range config.Mainnet_seed_nodes {

		connected := false

		for _, conn := range unique_map {
			if ParseIPNoError(conn.Addr.String()) == ParseIPNoError(ip) {
				connected = true

				version := conn.DaemonVersion
				if len(version) > 20 {
					version = version[:20]
				}

				fmt.Printf("\t%-22s Height: %d - Version: %s (Connected)\n", ip, conn.Height, version)
				break
			}
		}

		if !connected {
			fmt.Printf("\t%-22s\n", ip)
		}
	}
	fmt.Printf("\n")

	fmt.Printf("%-22s %-32s %-10s %-23s %-8s %-22s\n", "Address", "Added", "Connected", "Version", "Height", "Tag")

	trust_mutex.Lock()
	defer trust_mutex.Unlock()
	for Address, added := range trusted_map {

		found := false
		ip := ParseIPNoError(Address)
		for _, conn := range unique_map {
			if ParseIPNoError(conn.Addr.String()) == ip {
				found = true

				version := conn.DaemonVersion
				if len(version) > 20 {
					version = version[:20]
				}

				fmt.Printf("%-22s %-32s %-10s %-23s %-8d %-22s\n", Address, time.Unix(added, 0).Format(time.RFC1123), "Yes", version, conn.Height, conn.Tag)
				break
			}

		}

		if !found {
			fmt.Printf("%-22s %-32s %-10s\n", Address, time.Unix(added, 0).Format(time.RFC1123), "No")
		}

	}

	fmt.Printf("\n")

}

func Only_Trusted_Peers() {

	unique_map := UniqueConnections()
	for _, conn := range unique_map {
		if !conn.SyncNode && !IsTrustedIP(conn.Addr.String()) {
			logger.V(1).Info(fmt.Sprintf("Disconnecting: %s", conn.Addr.String()))
			conn.Client.Close()
			conn.Conn.Close()
			Connection_Delete(conn)
		}
	}

	fmt.Printf("\n")

}
