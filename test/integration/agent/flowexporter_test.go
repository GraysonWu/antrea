package agent

import (
	"fmt"
	"github.com/vmware-tanzu/antrea/pkg/agent/interfacestore"
	"net"
	"testing"
	"time"

	mock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vmware-tanzu/antrea/pkg/agent/flowexporter"
	"github.com/vmware-tanzu/antrea/pkg/agent/flowexporter/connections"
	connectionstest "github.com/vmware-tanzu/antrea/pkg/agent/flowexporter/connections/testing"
	"github.com/vmware-tanzu/antrea/pkg/agent/flowexporter/flowrecords"
	interfacestoretest "github.com/vmware-tanzu/antrea/pkg/agent/interfacestore/testing"
	"github.com/vmware-tanzu/antrea/pkg/agent/openflow"
)

func makeTuple(srcIP *net.IP, dstIP *net.IP, protoID uint8, srcPort uint16, dstPort uint16) (*flowexporter.Tuple, *flowexporter.Tuple) {
	tuple := &flowexporter.Tuple{
		SourceAddress:      *srcIP,
		DestinationAddress: *dstIP,
		Protocol:           protoID,
		SourcePort:         srcPort,
		DestinationPort:    dstPort,
	}
	revTuple := &flowexporter.Tuple{
		SourceAddress:      *dstIP,
		DestinationAddress: *srcIP,
		Protocol:           protoID,
		SourcePort:         dstPort,
		DestinationPort:    srcPort,
	}
	return tuple, revTuple
}

func createConnsForTest() ([]*flowexporter.Connection, []*flowexporter.ConnectionKey) {
	// Reference for flow timestamp
	refTime := time.Now()

	testConns := make([]*flowexporter.Connection, 2)
	testConnKeys := make([]*flowexporter.ConnectionKey, 2)
	// Flow-1
	tuple1, revTuple1 := makeTuple(&net.IP{1, 2, 3, 4}, &net.IP{4, 3, 2, 1}, 6, 65280, 255)
	testConn1 := &flowexporter.Connection{
		StartTime:       refTime.Add(-(time.Second * 50)),
		StopTime:        refTime,
		OriginalPackets: 0xffff,
		OriginalBytes:   0xbaaaaa0000000000,
		ReversePackets:  0xff,
		ReverseBytes:    0xbaaa,
		TupleOrig:       *tuple1,
		TupleReply:      *revTuple1,
	}
	testConnKey1 := flowexporter.NewConnectionKey(testConn1)
	testConns[0] = testConn1
	testConnKeys[0] = &testConnKey1
	// Flow-2
	tuple2, revTuple2 := makeTuple(&net.IP{5, 6, 7, 8}, &net.IP{8, 7, 6, 5}, 6, 60001, 200)
	testConn2 := &flowexporter.Connection{
		StartTime:       refTime.Add(-(time.Second * 20)),
		StopTime:        refTime,
		OriginalPackets: 0xbb,
		OriginalBytes:   0xcbbb,
		ReversePackets:  0xbbbb,
		ReverseBytes:    0xcbbbb0000000000,
		TupleOrig:       *tuple2,
		TupleReply:      *revTuple2,
	}
	testConnKey2 := flowexporter.NewConnectionKey(testConn2)
	testConns[1] = testConn2
	testConnKeys[1] = &testConnKey2

	return testConns, testConnKeys
}

func prepareInterfaceConfigs(contID, podName, podNS, ifName string, ip *net.IP) *interfacestore.InterfaceConfig {
	podConfig := &interfacestore.ContainerInterfaceConfig{
		ContainerID:  contID,
		PodName:      podName,
		PodNamespace: podNS,
	}
	iface := &interfacestore.InterfaceConfig{
		InterfaceName:            ifName,
		IP:                       *ip,
		ContainerInterfaceConfig: podConfig,
	}
	return iface
}

func testBuildFlowRecords(t *testing.T, flowRecords flowrecords.FlowRecords, conns []*flowexporter.Connection, connKeys []*flowexporter.ConnectionKey) {
	err := flowRecords.BuildFlowRecords()
	require.Nil(t, err, fmt.Sprintf("Failed to build flow records from connection store: %v", err))
	// Check if records in flow records are built as expected or not
	for i, expRecConn := range conns {
		actualRec, found := flowRecords.GetFlowRecordByConnKey(*connKeys[i])
		assert.Equal(t, found, true, "testConn should be part of flow records")
		assert.Equal(t, actualRec.Conn, expRecConn, "testConn and connection in connection store should be equal")
	}
}

// TestConnectionStoreAndFlowRecords covers two scenarios: (i.) Add connections to connection store through connectionStore.Poll
// execution and build flow records. (ii.) Flush the connections and check records are sti:w
func TestConnectionStoreAndFlowRecords(t *testing.T) {
	// Test setup
	ctrl := mock.NewController(t)
	defer ctrl.Finish()

	// Create ConnectionStore, FlowRecords and associated mocks
	connDumperMock := connectionstest.NewMockConnTrackDumper(ctrl)
	ifStoreMock := interfacestoretest.NewMockInterfaceStore(ctrl)
	// Hardcoded poll and export intervals; they are not used
	connStore := connections.NewConnectionStore(connDumperMock, ifStoreMock, time.Second, time.Second)
	flowRecords := flowrecords.NewFlowRecords(connStore)
	// Prepare connections and interface config for test
	testConns, testConnKeys := createConnsForTest()
	testIfConfigs := make([]*interfacestore.InterfaceConfig, 2)
	testIfConfigs[0] = prepareInterfaceConfigs("1", "pod1", "ns1", "interface1", &testConns[0].TupleOrig.SourceAddress)
	testIfConfigs[1] = prepareInterfaceConfigs("2", "pod2", "ns2", "interface2", &testConns[1].TupleOrig.DestinationAddress)

	// Expect calls for connStore.poll and other callees
	connDumperMock.EXPECT().DumpFlows(uint16(openflow.CtZone)).Return(testConns, nil)
	for i, testConn := range testConns {
		if i == 0 {
			ifStoreMock.EXPECT().GetInterfaceByIP(testConn.TupleOrig.SourceAddress.String()).Return(testIfConfigs[i], true)
			ifStoreMock.EXPECT().GetInterfaceByIP(testConn.TupleOrig.DestinationAddress.String()).Return(nil, false)
		} else {
			ifStoreMock.EXPECT().GetInterfaceByIP(testConn.TupleOrig.SourceAddress.String()).Return(nil, false)
			ifStoreMock.EXPECT().GetInterfaceByIP(testConn.TupleOrig.DestinationAddress.String()).Return(testIfConfigs[i], true)
		}
	}
	// Execute connStore.Poll
	connsLen, err := connStore.Poll()
	require.Nil(t, err, fmt.Sprintf("Failed to add connections to connection store: %v", err))
	assert.Equal(t, connsLen, len(testConns), "expected connections should be equal to number of testConns")

	// Check if connections in connectionStore are same as testConns or not
	for i, expConn := range testConns {
		if i == 0 {
			expConn.SourcePodName = testIfConfigs[i].PodName
			expConn.SourcePodNamespace = testIfConfigs[i].PodNamespace
		} else {
			expConn.DestinationPodName = testIfConfigs[i].PodName
			expConn.DestinationPodNamespace = testIfConfigs[i].PodNamespace
		}
		actualConn, found := connStore.GetConnByKey(*testConnKeys[i])
		assert.Equal(t, found, true, "testConn should be present in connection store")
		assert.Equal(t, expConn, actualConn, "testConn and connection in connection store should be equal")
	}

	// Test for build flow records
	testBuildFlowRecords(t, flowRecords, testConns, testConnKeys)

	// Delete the connections from connection store and check
	connStore.FlushConnectionStore()
	// Check the resulting connectionStore; connections should not be present in ConnectionStore
	for i := 0; i < len(testConns); i++ {
		_, found := connStore.GetConnByKey(*testConnKeys[i])
		assert.Equal(t, found, false, "testConn should not be part of connection store")
	}
	err = flowRecords.BuildFlowRecords()
	require.Nil(t, err, fmt.Sprintf("Failed to build flow records from connection store: %v", err))
	// Make sure that records corresponding to testConns are not gone in flow records.
	for i := 0; i < len(testConns); i++ {
		_, found := flowRecords.GetFlowRecordByConnKey(*testConnKeys[i])
		assert.Equal(t, found, true, "testConn should not be part of flow records")
	}

}
