package main

import (
	"fmt"
	"strings"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/provider"
	"thingue-launcher/server"
	"thingue-launcher/server/initialize"

	"github.com/spf13/cobra"
)

var (
	serverLogLevel string
	turnServer     string
	turnUsername   string
	turnPassword   string
)

func init() {
	serverCmd.Flags().StringVarP(&provider.AppConfig.LocalServer.BindAddr, "bindAddr", "b", "0.0.0.0:8877", "设置服务绑定的地址与端口")
	serverCmd.Flags().StringVar(&provider.AppConfig.LocalServer.ContentPath, "contentPath", "/", "设置服务路径前缀")
	serverCmd.Flags().StringVar(&serverLogLevel, "logLevel", "info", "设置日志级别")
	serverCmd.Flags().StringVar(&provider.AppConfig.LocalServer.StaticDir, "staticDir", "", "Path to directory containing the web static resources. Defaults use embed")
	serverCmd.Flags().StringVar(&turnServer, "turn-server", "", "turn服务地址")
	serverCmd.Flags().StringVar(&turnUsername, "turn-username", "", "turn服务用户名")
	serverCmd.Flags().StringVar(&turnPassword, "turn-password", "", "turn服务密码")
	rootCmd.AddCommand(serverCmd)
}

// const tmpl = `iceServers:
//   - urls:
//   - turn:%s
//     username: %s
//     credential: %s`
const tmpl = `iceServers:
  - urls:%s
    username: %s
    credential: %s`

var serverCmd = &cobra.Command{
	Use:   `server`,
	Short: "运行信令服务",
	RunE: func(cmd *cobra.Command, args []string) error {
		if turnServer != "" && turnUsername != "" && turnPassword != "" {
			var turnUrls string
			servers := strings.Split(turnServer, ",")
			if len(servers) > 1 {
				for _, server := range servers {
					turnUrls += fmt.Sprintf("\n    - turn:%s", server)
				}
			} else {
				turnUrls = fmt.Sprintf("\n    - turn:%s", turnServer)
			}
			peerConnectionOptions := fmt.Sprintf(tmpl, turnUrls, turnUsername, turnPassword)
			fmt.Println(peerConnectionOptions)
			provider.AppConfig.PeerConnectionOptions = peerConnectionOptions
		}
		server.Init()
		logger.InitZapLogger(serverLogLevel, "server.log")
		initialize.Server.Serve()
		return nil
	},
}
