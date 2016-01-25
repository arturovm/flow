package networking

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"

	"github.com/hashicorp/mdns"
)

// LookupPeers busca peers en la red
func LookupPeers() <-chan []string {
	entriesCh := make(chan *mdns.ServiceEntry, 4)
	resChan := make(chan []string, 1)
	go func(ch chan<- []string) {
		var entries []string             // peers
		addrs, _ := net.InterfaceAddrs() // interfaces locales
		for entry := range entriesCh {
			addr := entry.AddrV4 // dirección de host encontrado
			skip := false        // saltar o no esta dirección
			// buscar si peer encontrado es máquina propia
			for i := range addrs {
				n := addrs[i].(*net.IPNet) // hacer type assertion
				if n.IP.Equal(addr) {
					skip = true
				}
			}
			if !skip {
				entries = append(entries, net.JoinHostPort(addr.String(),
					fmt.Sprintf("%d", entry.Port)))
			}
		}
		resChan <- entries
	}(resChan)
	mdns.Lookup("_flow._tcp", entriesCh)
	close(entriesCh)
	return resChan
}

func getUsage(peerAddr string) (float64, error) {
	conn, err := net.Dial("tcp", peerAddr)
	if err != nil {
		return 0.0, fmt.Errorf("error connecting to host: %s", err)
	}
	conn.Write([]byte("usage"))
	buf := make([]byte, 64)
	_, err = conn.Read(buf)
	if err != nil {
		return 0.0, fmt.Errorf("error reading usage response: %s", err.Error())
	}
	n := bytes.Index(buf, []byte{0})
	reply := string(buf[:n])
	u, err := strconv.ParseFloat(reply, 64)
	if err != nil {
		return 0.0, fmt.Errorf("error parsing usage: %s", err.Error())
	}
	return u, nil
}

func selectPeer() (string, error) {
	c := LookupPeers()
	peers := <-c
	if len(peers) > 0 {
		var peerSelected string
		for i := range peers {
			u, err := getUsage(peers[i])
			if err != nil {
				return "", err
			}
			if u < 20.0 {
				peerSelected = peers[i]
				break
			}
			if i == len(peers)-1 {
				return "", errors.New("no suitable peer found")
			}
		}
		return peerSelected, nil
	}
	return "", errors.New("no peers found")
}

// SendEval se encarga
func SendEval(evalMsg string) {
	fmt.Println("sending eval msg")
	peer, err := selectPeer()
	if err != nil {
		out <- Event{
			Type: Error,
			Data: fmt.Sprintf("error selecting peer: %s", err.Error()),
		}
		return
	}
	fmt.Println("selected peer")
	conn, err := net.Dial("tcp", peer)
	if err != nil {
		out <- Event{
			Type: Error,
			Data: fmt.Sprintf("error connecting to host: %s", err),
		}
		return
	}
	fmt.Println("writing eval msg")
	conn.Write([]byte(evalMsg))
	fmt.Println("wrote eval msg")
	result := readEvalResult(conn)
	out <- Event{
		Type: GotEvalReply,
		Data: result,
	}
}

func readEvalResult(conn net.Conn) string {
	fmt.Println("reading eval result")
	buf := make([]byte, 1024)
	_, err := conn.Read(buf)
	conn.Close()
	if err != nil {
		log.Printf("error reading: %s", err.Error())
	}
	n := bytes.Index(buf, []byte{0})
	reply := string(buf[:n])
	log.Println("got eval reply")
	return reply
}
