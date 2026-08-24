package provider

import (
	"errors"
	"github.com/gorilla/websocket"
	"sync"
	"thingue-launcher/common/logger"
	"time"
)

type sdpConnProvider struct {
	playerIdCount uint
	idStreamerMap map[string]*StreamerConnector
	idPlayerMap   map[uint]*PlayerConnector
	restartingMap map[string]bool
	mapLock       sync.RWMutex // 保护 idStreamerMap/idPlayerMap 的增删与遍历
	stateLock     sync.Mutex   // 保护 restartingMap
}

var SdpConnProvider = sdpConnProvider{
	idStreamerMap: make(map[string]*StreamerConnector),
	idPlayerMap:   make(map[uint]*PlayerConnector),
	restartingMap: make(map[string]bool),
}

func (sdp *sdpConnProvider) NewStreamer(sid string, conn *websocket.Conn, enableRelay bool, enableRenderControl bool) *StreamerConnector {
	streamer := &StreamerConnector{
		SID:                 sid,
		conn:                conn,
		PlayerConnectors:    make([]*PlayerConnector, 0),
		AutoStopTimer:       time.NewTimer(999 * time.Second),
		EnableRelay:         enableRelay,
		EnableRenderControl: enableRenderControl,
	}
	streamer.AutoStopTimer.Stop()
	sdp.mapLock.Lock()
	sdp.idStreamerMap[streamer.SID] = streamer
	sdp.mapLock.Unlock()
	return streamer
}

func (sdp *sdpConnProvider) RemoveStreamer(sid string) {
	sdp.mapLock.Lock()
	delete(sdp.idStreamerMap, sid)
	sdp.mapLock.Unlock()
}

func (sdp *sdpConnProvider) NewPlayer(conn *websocket.Conn) *PlayerConnector {
	sdp.mapLock.Lock()
	sdp.playerIdCount++
	player := &PlayerConnector{
		PlayerId: sdp.playerIdCount,
		conn:     conn,
	}
	sdp.idPlayerMap[player.PlayerId] = player
	sdp.mapLock.Unlock()
	return player
}

func (sdp *sdpConnProvider) RemovePlayer(id uint) {
	sdp.mapLock.Lock()
	delete(sdp.idPlayerMap, id)
	sdp.mapLock.Unlock()
}

func (sdp *sdpConnProvider) GetStreamer(id string) (*StreamerConnector, error) {
	sdp.mapLock.RLock()
	defer sdp.mapLock.RUnlock()
	connector, ok := sdp.idStreamerMap[id]
	if ok {
		return connector, nil
	} else {
		return nil, errors.New("streamer不存在")
	}
}

func (sdp *sdpConnProvider) GetPlayer(id uint) (*PlayerConnector, error) {
	sdp.mapLock.RLock()
	defer sdp.mapLock.RUnlock()
	connector, ok := sdp.idPlayerMap[id]
	if ok {
		return connector, nil
	} else {
		return nil, errors.New("player不存在")
	}
}

func (sdp *sdpConnProvider) GetPlayersByUserData(userMap map[string]string) []*PlayerConnector {
	sdp.mapLock.RLock()
	defer sdp.mapLock.RUnlock()
	var players []*PlayerConnector
	for _, connector := range sdp.idPlayerMap {
		matched := true
		for key, value := range userMap {
			if connector.UserData[key] != value {
				matched = false
				break
			}
		}
		if matched {
			players = append(players, connector)
		}
	}
	return players
}

func (sdp *sdpConnProvider) GetStreamerRestartingState(id string) bool {
	sdp.stateLock.Lock()
	defer sdp.stateLock.Unlock()
	state, ok := sdp.restartingMap[id]
	if ok {
		return state
	}
	return false
}

func (sdp *sdpConnProvider) SetStreamerRestartingState(id string, state bool) {
	sdp.stateLock.Lock()
	defer sdp.stateLock.Unlock()
	logger.Zap.Infof("设置实例重启中标识 %s %t", id, state)
	if state {
		sdp.restartingMap[id] = state
	} else {
		delete(sdp.restartingMap, id)
	}
}
