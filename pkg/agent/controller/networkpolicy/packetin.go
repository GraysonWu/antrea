// Copyright 2020 Antrea Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package networkpolicy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/contiv/libOpenflow/openflow13"
	"github.com/contiv/libOpenflow/protocol"
	"github.com/contiv/libOpenflow/util"
	"github.com/contiv/ofnet/ofctrl"
	"gopkg.in/natefinch/lumberjack.v2"
	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/agent/openflow"
	opsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/ops/v1alpha1"
	binding "github.com/vmware-tanzu/antrea/pkg/ovs/openflow"
)

const (
	logDir      string = "/var/log/antrea/networkpolicy/"
	logfileName string = "np.log"

	EthHdrLen  uint16 = 14
	IPv4HdrLen uint16 = 20
	IPv6HdrLen uint16 = 40
	TCPHdrLen  uint16 = 20

	ICMPUnusedHdrLen uint16 = 4
)

var (
	AntreaPolicyLogger *log.Logger
)

// logInfo will be set by retrieving info from packetin and register
type logInfo struct {
	tableName   string // name of the table sending packetin
	npRef       string // Network Policy name reference for Antrea NetworkPolicy
	disposition string // Allow/Drop of the rule sending packetin
	ofPriority  string // openflow priority of the flow sending packetin
	srcIP       string // source IP of the traffic logged
	destIP      string // destination IP of the traffic logged
	pktLength   uint16 // packet length of packetin
	protocolStr string // protocol of the traffic logged
}

// initLogger is called while newing Antrea network policy agent controller.
// Customize AntreaPolicyLogger specifically for Antrea Policies audit logging.
func initLogger() error {
	// logging file should be /var/log/antrea/networkpolicy/np.log
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		os.Mkdir(logDir, 0755)
	}
	file, err := os.OpenFile(logDir+logfileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		klog.Errorf("Failed to initialize logger to audit Antrea Policies %v", err)
		return err
	}

	AntreaPolicyLogger = log.New(file, "", log.Ldate|log.Lmicroseconds)
	// Use lumberjack log file rotation
	AntreaPolicyLogger.SetOutput(&lumberjack.Logger{
		Filename:   logDir + logfileName,
		MaxSize:    500,  // allow max 500 megabytes for one log file
		MaxBackups: 3,    // allow max 3 old log file backups
		MaxAge:     28,   // allow max 28 days maintenance of old log files
		Compress:   true, // compress the old log files for backup
	})
	klog.V(2).Info("Initialized Antrea-native Policy Logger for audit logging")
	return nil
}

// HandlePacketIn is the packetin handler registered to openflow by Antrea network
// policy agent controller. It performs the appropriate operations based on which
// bits are set in the "custom reasons" field of the packet received from OVS.
func (c *Controller) HandlePacketIn(pktIn *ofctrl.PacketIn) error {
	if pktIn == nil {
		return errors.New("empty packetin for Antrea Policy")
	}

	matchers := pktIn.GetMatches()
	// Get custom reasons in this packet-in.
	match := getMatchRegField(matchers, uint32(openflow.CustomReasonMarkReg))
	customReasons, err := getInfoInReg(match, openflow.CustomReasonMarkRange.ToNXRange())
	if err != nil {
		return fmt.Errorf("received error while unloading customReason from reg: %v", err)
	}

	// Use reasons to choose operations.
	if customReasons&openflow.CustomReasonLogging == openflow.CustomReasonLogging {
		if err := c.logPacket(pktIn); err != nil {
			return err
		}
	}
	if customReasons&openflow.CustomReasonReject == openflow.CustomReasonReject {
		if err := c.rejectRequest(pktIn); err != nil {
			return err
		}
	}

	return nil
}

// logPacket retrieves information from openflow reg, controller cache, packet-in
// packet to log.
func (c *Controller) logPacket(pktIn *ofctrl.PacketIn) error {
	ob := new(logInfo)

	// Get Network Policy log info
	err := getNetworkPolicyInfo(pktIn, c, ob)
	if err != nil {
		return fmt.Errorf("received error while retrieving NetworkPolicy info: %v", err)
	}

	// Get packet log info
	err = getPacketInfo(pktIn, ob)
	if err != nil {
		return fmt.Errorf("received error while handling packetin for NetworkPolicy: %v", err)
	}

	// Store log file
	AntreaPolicyLogger.Printf("%s %s %s %s SRC: %s DEST: %s %d %s", ob.tableName, ob.npRef, ob.disposition, ob.ofPriority, ob.srcIP, ob.destIP, ob.pktLength, ob.protocolStr)
	return nil
}

// getMatchRegField returns match to the regNum register.
func getMatchRegField(matchers *ofctrl.Matchers, regNum uint32) *ofctrl.MatchField {
	return matchers.GetMatchByName(fmt.Sprintf("NXM_NX_REG%d", regNum))
}

// getMatch receives ofctrl matchers and table id, match field.
// Modifies match field to Ingress/Egress register based on tableID.
func getMatch(matchers *ofctrl.Matchers, tableID binding.TableIDType, disposition uint32) *ofctrl.MatchField {
	// Get match from CNPNotAllowConjIDReg if disposition is not allow.
	if disposition != openflow.DispositionAllow {
		return getMatchRegField(matchers, uint32(openflow.CNPNotAllowConjIDReg))
	}
	// Get match from ingress/egress reg if disposition is allow
	for _, table := range append(openflow.GetAntreaPolicyEgressTables(), openflow.EgressRuleTable) {
		if tableID == table {
			return getMatchRegField(matchers, uint32(openflow.EgressReg))
		}
	}
	for _, table := range append(openflow.GetAntreaPolicyIngressTables(), openflow.IngressRuleTable) {
		if tableID == table {
			return getMatchRegField(matchers, uint32(openflow.IngressReg))
		}
	}
	return nil
}

// getInfoInReg unloads and returns data stored in the match field.
func getInfoInReg(regMatch *ofctrl.MatchField, rng *openflow13.NXRange) (uint32, error) {
	regValue, ok := regMatch.GetValue().(*ofctrl.NXRegister)
	if !ok {
		return 0, errors.New("register value cannot be retrieved")
	}
	if rng != nil {
		return ofctrl.GetUint32ValueWithRange(regValue.Data, rng), nil
	}
	return regValue.Data, nil
}

// getNetworkPolicyInfo fills in tableName, npName, ofPriority, disposition of logInfo ob.
func getNetworkPolicyInfo(pktIn *ofctrl.PacketIn, c *Controller, ob *logInfo) error {
	matchers := pktIn.GetMatches()
	var match *ofctrl.MatchField
	// Get table name
	tableID := binding.TableIDType(pktIn.TableId)
	ob.tableName = openflow.GetFlowTableName(tableID)

	// Get disposition Allow or Drop
	match = getMatchRegField(matchers, uint32(openflow.DispositionMarkReg))
	info, err := getInfoInReg(match, openflow.APDispositionMarkRange.ToNXRange())
	if err != nil {
		return errors.New(fmt.Sprintf("received error while unloading disposition from reg: %v", err))
	}
	ob.disposition = openflow.DispositionToString[info]

	// Set match to corresponding ingress/egress reg according to disposition
	match = getMatch(matchers, tableID, info)

	// Get Network Policy full name and OF priority of the conjunction
	info, err = getInfoInReg(match, nil)
	if err != nil {
		return errors.New(fmt.Sprintf("received error while unloading conjunction id from reg: %v", err))
	}
	ob.npRef, ob.ofPriority = c.ofClient.GetPolicyInfoFromConjunction(info)

	return nil
}

// getPacketInfo fills in srcIP, destIP, pktLength, protocol of logInfo ob.
func getPacketInfo(pktIn *ofctrl.PacketIn, ob *logInfo) error {
	// TODO: supprt IPv6 packet
	if pktIn.Data.Ethertype == opsv1alpha1.EtherTypeIPv4 {
		ipPacket, ok := pktIn.Data.Data.(*protocol.IPv4)
		if !ok {
			return errors.New("invalid IPv4 packet")
		}
		// Get source destination IP and protocol
		ob.srcIP = ipPacket.NWSrc.String()
		ob.destIP = ipPacket.NWDst.String()
		ob.pktLength = ipPacket.Length
		ob.protocolStr = opsv1alpha1.ProtocolsToString[int32(ipPacket.Protocol)]
	}
	return nil
}

// rejectRequest sends reject response to the requesting client, based on the
// packet-in message.
func (c *Controller) rejectRequest(pktIn *ofctrl.PacketIn) error {
	// Get ethernet data.
	srcMAC := pktIn.Data.HWDst
	dstMAC := pktIn.Data.HWSrc

	var (
		srcIP    string
		dstIP    string
		prot     uint8
		ipHdrLen uint16
	)
	switch pktIn.Data.Ethertype {
	case protocol.IPv4_MSG:
		// Get IP data.
		ipv4In := pktIn.Data.Data.(*protocol.IPv4)
		srcIP = ipv4In.NWDst.String()
		dstIP = ipv4In.NWSrc.String()
		prot = ipv4In.Protocol
		ipHdrLen = IPv4HdrLen
	case protocol.IPv6_MSG:
		// Get IP data.
		ipv6In := pktIn.Data.Data.(*protocol.IPv6)
		srcIP = ipv6In.NWDst.String()
		dstIP = ipv6In.NWSrc.String()
		prot = ipv6In.NextHeader
		ipHdrLen = IPv6HdrLen
	}

	pktOutBuilder, err := c.getBasePacketOutBuilder(srcMAC.String(), dstMAC.String(), srcIP, dstIP)
	if err != nil {
		return err
	}

	switch prot {
	case protocol.Type_TCP:
		// Get TCP data.
		TCPSrcPort, TCPDstPort, TCPSeqNum, TCPAckNum, err := getTCPHeaderData(pktIn.Data.Data)
		if err != nil {
			return err
		}
		return c.ofClient.SendTCPReject(pktOutBuilder, TCPSrcPort, TCPDstPort, TCPSeqNum, TCPAckNum, false)
	default: // Use ICMP host administratively prohibited for ICMP, UDP, SCTP reject.
		ipHdr, _ := pktIn.Data.Data.MarshalBinary()
		ICMPData := make([]byte, int(ICMPUnusedHdrLen+ipHdrLen+8))
		// Put ICMP unused header in Data prop and set it to zero.
		binary.BigEndian.PutUint32(ICMPData[:ICMPUnusedHdrLen], 0)
		copy(ICMPData[ICMPUnusedHdrLen:], ipHdr[:ipHdrLen+8])
		return c.ofClient.SendICMPReject(pktOutBuilder, ICMPData, false)
	}
}

// getTCPHeaderData gets TCP header data used in the reject packet.
func getTCPHeaderData(ipPkt util.Message) (TCPSrcPort uint16, TCPDstPort uint16, TCPSeqNum uint32, TCPAckNum uint32, err error) {
	var tcpBytes []byte

	// Transfer Buffer to TCP
	switch ipPkt.(type) {
	case *protocol.IPv4:
		tcpBytes, err = ipPkt.(*protocol.IPv4).Data.(*util.Buffer).MarshalBinary()
	case *protocol.IPv6:
		tcpBytes, err = ipPkt.(*protocol.IPv6).Data.(*util.Buffer).MarshalBinary()
	}
	if err != nil {
		return 0, 0, 0, 0, err
	}
	tcpIn := new(protocol.TCP)
	err = tcpIn.UnmarshalBinary(tcpBytes)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	return tcpIn.PortDst, tcpIn.PortSrc, 0, tcpIn.SeqNum + 1, nil
}

// getBasePacketOutBuilder gets a base IP packetOutBuilder with src&dst MAC, IP and OFPort set.
func (c *Controller) getBasePacketOutBuilder(srcMAC string, dstMAC string, srcIP string, dstIP string) (binding.PacketOutBuilder, error) {

	// Get the OpenFlow ports of src Pod and dst Pod.
	sIface, srcFound := c.ifaceStore.GetInterfaceByIP(srcIP)
	dIface, dstFound := c.ifaceStore.GetInterfaceByIP(dstIP)
	if !srcFound || !dstFound {
		return nil, fmt.Errorf("couldn't find Pod config by IP")
	}

	packetOutBuilder, err := c.ofClient.GenBasePacketOutBuilder(srcMAC, dstMAC, srcIP, dstIP, uint32(sIface.OFPort), uint32(dIface.OFPort))
	if err != nil {
		return nil, err
	}

	return packetOutBuilder, nil
}
