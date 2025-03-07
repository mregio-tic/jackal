// Copyright 2020 The jackal Authors
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

package main

import (
	"path/filepath"
	"time"

	"github.com/ortuman/jackal/pkg/module/offline"

	"github.com/ortuman/jackal/pkg/module/xep0092"

	"github.com/ortuman/jackal/pkg/module/xep0198"

	"github.com/ortuman/jackal/pkg/module/xep0199"

	"github.com/ortuman/jackal/pkg/shaper"

	"github.com/kkyr/fig"
	adminserver "github.com/ortuman/jackal/pkg/admin/server"
	"github.com/ortuman/jackal/pkg/auth/pepper"
	"github.com/ortuman/jackal/pkg/cluster/etcd"
	clusterserver "github.com/ortuman/jackal/pkg/cluster/server"
	"github.com/ortuman/jackal/pkg/host"
	"github.com/ortuman/jackal/pkg/storage"
)

type listenerConfig struct {
	Type      string `fig:"type" default:"c2s"`
	BindAddr  string `fig:"bind_addr"`
	Port      int    `fig:"port" default:"5222"`
	Transport string `fig:"transport" default:"socket"`
	DirectTLS bool   `fig:"direct_tls"`
	SASL      struct {
		Mechanisms []string `fig:"mechanisms" default:"[scram_sha_1, scram_sha_256, scram_sha_512, scram_sha3_512]"`
		External   struct {
			Address  string `fig:"address"`
			IsSecure bool   `fig:"is_secure"`
		} `fig:"external"`
	} `fig:"sasl"`
	CompressionLevel    string        `fig:"compression_level" default:"default"`
	ResourceConflict    string        `fig:"resource_conflict" default:"terminate_old"`
	MaxStanzaSize       int           `fig:"max_stanza_size" default:"32768"`
	Secret              string        `fig:"secret"`
	ConnectTimeout      time.Duration `fig:"conn_timeout" default:"3s"`
	AuthenticateTimeout time.Duration `fig:"auth_timeout" default:"10s"`
	KeepAliveTimeout    time.Duration `fig:"keep_alive_timeout" default:"10m"`
	RequestTimeout      time.Duration `fig:"req_timeout" default:"15s"`
}

type s2sOutConfig struct {
	DialTimeout      time.Duration `fig:"dial_timeout" default:"5s"`
	DialbackSecret   string        `fig:"secret"`
	ConnectTimeout   time.Duration `fig:"conn_timeout" default:"3s"`
	KeepAliveTimeout time.Duration `fig:"keep_alive_timeout" default:"120s"`
	RequestTimeout   time.Duration `fig:"req_timeout" default:"15s"`
	MaxStanzaSize    int           `fig:"max_stanza_size" default:"131072"`
}

type modulesConfig struct {
	Enabled []string `fig:"enabled"`

	Offline offline.Config `fig:"offline"`

	// XEP-0092: Software Version
	Version xep0092.Config `fig:"version"`

	// XEP-0198: Stream Management
	Stream xep0198.Config `fig:"stream"`

	// XEP-0199: XMPP Ping
	Ping xep0199.Config `fig:"ping"`
}

type componentsConfig struct{}

type serverConfig struct {
	Logger struct {
		Level      string `fig:"level" default:"debug"`
		OutputPath string `fig:"output_path"`
	} `fig:"logger"`

	Cluster struct {
		Etcd   etcd.Config          `fig:"etcd"`
		Server clusterserver.Config `fig:"server"`
	} `fig:"cluster"`

	HTTPPort int `fig:"http_port" default:"6060"`

	Peppers    pepper.Config      `fig:"peppers"`
	Admin      adminserver.Config `fig:"admin"`
	Storage    storage.Config     `fig:"storage"`
	Hosts      []host.Config      `fig:"hosts"`
	Shapers    []shaper.Config    `fig:"shapers"`
	Listeners  []listenerConfig   `fig:"listeners"`
	S2SOut     s2sOutConfig       `fig:"s2s_out"`
	Modules    modulesConfig      `fig:"modules"`
	Components componentsConfig   `fig:"components"`
}

func loadConfig(configFile string) (*serverConfig, error) {
	var cfg serverConfig
	file := filepath.Base(configFile)
	dir := filepath.Dir(configFile)

	err := fig.Load(&cfg, fig.File(file), fig.Dirs(dir))
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
