package main

import (
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"net"
	"strings"
	"sync"
	"time"
)

// тут вы пишете код
// обращаю ваше внимание - в этом задании запрещены глобальные переменные

func authStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	md, _ := metadata.FromIncomingContext(ss.Context())
	p, _ := peer.FromContext(ss.Context())
	consumer, ok := md["consumer"]
	m, _ := srv.(*MyMicroservice)
	if !ok || !m.hasAccess(consumer[0], info.FullMethod) {
		return status.Error(codes.Unauthenticated, "No consumer information in request")
	}
	m.addInvocation(info.FullMethod, consumer[0])
	m.logEvent(Event{Timestamp: time.Now().UnixNano(), Consumer: consumer[0], Method: info.FullMethod, Host: p.Addr.String()})
	return handler(srv, ss)
}
func authInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {

	md, _ := metadata.FromIncomingContext(ctx)
	p, _ := peer.FromContext(ctx)
	consumer, ok := md["consumer"]
	m, _ := info.Server.(*MyMicroservice)
	fmt.Printf("method %s \n", info.FullMethod)
	if !ok || !m.hasAccess(consumer[0], info.FullMethod) {
		return nil, status.Error(codes.Unauthenticated, "No consumer information in request")
	}

	reply, err := handler(ctx, req)
	m.addInvocation(info.FullMethod, consumer[0])
	m.logEvent(Event{Timestamp: time.Now().UnixNano(), Consumer: consumer[0], Method: info.FullMethod, Host: p.Addr.String()})
	return reply, err
}

type MyMicroservice struct {
	AclData                map[string][]string
	LogMutex               *sync.RWMutex
	LogListeners           map[int]chan *Event
	LogCount               int
	StatMutex      *sync.RWMutex
	StatCount int
	Stat map[int]Stat

}


func (m *MyMicroservice) registerLogClient() (int, chan *Event) {
	m.LogMutex.Lock()
	defer m.LogMutex.Unlock()
	m.LogCount++
	m.LogListeners[m.LogCount] = make(chan *Event)
	return m.LogCount, m.LogListeners[m.LogCount]
}

func (m *MyMicroservice) logEvent(event Event) {
	fmt.Printf("Logging event %#v \n", event)
	m.LogMutex.RLock()
	defer m.LogMutex.RUnlock()
	for client, c := range m.LogListeners {
		fmt.Printf("Notification to client %v\n", client)
		c <- &event
	}
}
func (m *MyMicroservice) unsubscribeLogClient(clientId int) {
	fmt.Printf("Unsubscribing client %v\n", clientId)
	m.LogMutex.Lock()
	defer m.LogMutex.Unlock()
	delete(m.LogListeners, clientId)
}

func (m *MyMicroservice) registerStatClient() (int) {
	m.StatMutex.Lock()
	defer m.StatMutex.Unlock()
	m.StatCount++
	m.Stat[m.StatCount]=Stat{ByConsumer:make(map[string]uint64), ByMethod:make(map[string]uint64)}
	return m.StatCount
}

func (m *MyMicroservice) unsubscribeStatClient(client int) {
	m.StatMutex.Lock()
	defer m.StatMutex.Unlock()
	delete(m.Stat, client)
}

func (m *MyMicroservice) resetStat(client int) {
	m.StatMutex.Lock()
	defer m.StatMutex.Unlock()
	m.Stat[client]=Stat{ByConsumer:make(map[string]uint64), ByMethod:make(map[string]uint64)}
}

func (m *MyMicroservice) getStat(client int) Stat {
	m.StatMutex.RLock()
	defer m.StatMutex.RUnlock()
	s := m.Stat[client]
	s.Timestamp = time.Now().UnixNano()
	return s
}
func (m *MyMicroservice) addInvocation(method string, consumer string) {
	m.StatMutex.Lock()
	defer m.StatMutex.Unlock()
	for _, stat := range m.Stat {
		stat.ByMethod[method]++
		stat.ByConsumer[consumer]++
	}
}
func (m *MyMicroservice) hasAccess(consumerName string, methodName string) bool {
	fmt.Printf("checking method %s access for conxumer %s \n", methodName, consumerName)
	methods, ok := m.AclData[consumerName]
	if !ok {
		return false
	}
	for _, method := range methods {
		if methodName == method {
			return true
		}
		if strings.HasSuffix(method, "*") {
			prefix := strings.TrimSuffix(method, "*")
			if strings.HasPrefix(methodName, prefix) {
				return true
			}
		}
	}
	return false
}

func newMyMicroservice() *MyMicroservice {
	result := new(MyMicroservice)
	result.LogMutex = &sync.RWMutex{}
	result.LogListeners = make(map[int]chan *Event)
	result.StatMutex = &sync.RWMutex{}
	result.Stat = make(map[int]Stat)
	return result
}
func StartMyMicroservice(context context.Context, listendAddress string, aclData string) error {
	myMicroservice := newMyMicroservice()
	err := json.Unmarshal([]byte(aclData), &myMicroservice.AclData)
	if err != nil {
		return err
	}
	lis, err := net.Listen("tcp", listendAddress)
	server := grpc.NewServer(grpc.UnaryInterceptor(authInterceptor), grpc.StreamInterceptor(authStreamInterceptor))

	RegisterAdminServer(server, myMicroservice)
	RegisterBizServer(server, myMicroservice)
	go func() {
		select {
		case <-context.Done():
			println("closing server")
			server.Stop()
		}
	}()
	println("Starting server on ", listendAddress)
	go func() {
		server.Serve(lis)
	}()
	return err
}

func (*MyMicroservice) Check(context.Context, *Nothing) (*Nothing, error) {
	return &Nothing{Dummy: true}, nil
}

func (*MyMicroservice) Add(context.Context, *Nothing) (*Nothing, error) {
	return &Nothing{Dummy: true}, nil
}
func (*MyMicroservice) Test(context.Context, *Nothing) (*Nothing, error) {
	return &Nothing{Dummy: true}, nil
}

func (m *MyMicroservice) Logging(nothing *Nothing, server Admin_LoggingServer) error {
	fmt.Printf("Logging \n")
	clientId, notificationChanel := m.registerLogClient()
	fmt.Printf("Registered log client %d \n", clientId)
	for {
		msg := <-notificationChanel
		fmt.Printf("sending msg %#v to client %v", msg, clientId)
		err := server.Send(msg)
		if err != nil {
			return err
		}
	}

}

func (m *MyMicroservice) Statistics(statInterval *StatInterval, server Admin_StatisticsServer) error {
	fmt.Printf("Statistics ")
	interval := statInterval.IntervalSeconds
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	clientId := m.registerStatClient()
	for {
		<-ticker.C
		stat := m.getStat(clientId)
		err := server.Send(&stat)
		m.resetStat(clientId)
		if err != nil {
			m.unsubscribeStatClient(clientId)
			return err
		}
	}
}



