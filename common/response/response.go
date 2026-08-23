package response

import (
	"thingue-launcher/common/domain"
	"thingue-launcher/common/model"

	"github.com/mitchellh/mapstructure"
	"k8s.io/apimachinery/pkg/labels"
)

type InstanceTicket struct {
	CID               uint                `json:"cid"` //客户端唯一ID
	SID               string              `json:"sid"` //服务端唯一ID
	Name              string              `json:"name"`
	SceneId           string              `json:"sceneID"` //实例元数据
	Pid               int                 `json:"pid"`
	StateCode         int8                `json:"stateCode"`
	StreamerConnected bool                `json:"streamerConnected"`
	Labels            labels.Labels       `json:"labels"`
	Ticket                 string              `json:"ticket"`
	PlayerConfig           domain.PlayerConfig `json:"playerConfig"`
	LicenseExpireDate      string              `json:"licenseExpireDate,omitempty"`
	LicenseRemainingDays   float64             `json:"licenseRemainingDays,omitempty"`
	LicenseApplicationCode string              `json:"licenseApplicationCode,omitempty"`
}

func (t *InstanceTicket) SetInstanceInfo(instance *model.ServerInstance) {
	mapstructure.Decode(instance, t)
}
