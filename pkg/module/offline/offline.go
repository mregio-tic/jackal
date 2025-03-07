// Copyright 2021 The jackal Authors
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

package offline

import (
	"context"
	"fmt"
	"time"

	"github.com/jackal-xmpp/stravaganza/v2"
	stanzaerror "github.com/jackal-xmpp/stravaganza/v2/errors/stanza"
	"github.com/ortuman/jackal/pkg/c2s"
	"github.com/ortuman/jackal/pkg/cluster/locker"
	"github.com/ortuman/jackal/pkg/hook"
	"github.com/ortuman/jackal/pkg/host"
	"github.com/ortuman/jackal/pkg/log"
	"github.com/ortuman/jackal/pkg/router"
	"github.com/ortuman/jackal/pkg/storage/repository"
	xmpputil "github.com/ortuman/jackal/pkg/util/xmpp"
)

const (
	offlineFeature = "msgoffline"

	hintsNamespace = "urn:xmpp:hints"
)

// ModuleName represents offline module name.
const ModuleName = "offline"

// Config contains offline module configuration value.
type Config struct {
	// QueueSize defines maximum offline queue size.
	QueueSize int `fig:"queue_size" default:"200"`
}

// Offline represents offline module type.
type Offline struct {
	cfg    Config
	hosts  hosts
	router router.Router
	resMng resourceManager
	rep    repository.Offline
	locker locker.Locker
	hk     *hook.Hooks
}

// New creates and initializes a new Offline instance.
func New(
	cfg Config,
	router router.Router,
	hosts *host.Hosts,
	resMng *c2s.ResourceManager,
	rep repository.Offline,
	locker locker.Locker,
	hk *hook.Hooks,
) *Offline {
	return &Offline{
		cfg:    cfg,
		router: router,
		hosts:  hosts,
		resMng: resMng,
		rep:    rep,
		locker: locker,
		hk:     hk,
	}
}

// Name returns offline module name.
func (m *Offline) Name() string { return ModuleName }

// StreamFeature returns offline module stream feature.
func (m *Offline) StreamFeature(_ context.Context, _ string) (stravaganza.Element, error) {
	return nil, nil
}

// ServerFeatures returns offline module server disco features.
func (m *Offline) ServerFeatures(_ context.Context) ([]string, error) {
	return []string{offlineFeature}, nil
}

// AccountFeatures returns offline module account disco features.
func (m *Offline) AccountFeatures(_ context.Context) ([]string, error) { return nil, nil }

// Start starts offline module.
func (m *Offline) Start(_ context.Context) error {
	m.hk.AddHook(hook.C2SStreamWillRouteElement, m.onWillRouteElement, hook.LowestPriority)
	m.hk.AddHook(hook.S2SInStreamWillRouteElement, m.onWillRouteElement, hook.LowestPriority)

	m.hk.AddHook(hook.C2SStreamPresenceReceived, m.onC2SPresenceRecv, hook.DefaultPriority)
	m.hk.AddHook(hook.UserDeleted, m.onUserDeleted, hook.DefaultPriority)

	log.Infow("Started offline module", "xep", ModuleName)
	return nil
}

// Stop stops offline module.
func (m *Offline) Stop(_ context.Context) error {
	m.hk.RemoveHook(hook.C2SStreamWillRouteElement, m.onWillRouteElement)
	m.hk.RemoveHook(hook.S2SInStreamWillRouteElement, m.onWillRouteElement)

	m.hk.RemoveHook(hook.C2SStreamPresenceReceived, m.onC2SPresenceRecv)
	m.hk.RemoveHook(hook.UserDeleted, m.onUserDeleted)

	log.Infow("Stopped offline module", "xep", ModuleName)
	return nil
}

func (m *Offline) onWillRouteElement(ctx context.Context, execCtx *hook.ExecutionContext) error {
	var elem stravaganza.Element

	switch inf := execCtx.Info.(type) {
	case *hook.C2SStreamInfo:
		elem = inf.Element
	case *hook.S2SStreamInfo:
		elem = inf.Element
	}
	msg, ok := elem.(*stravaganza.Message)
	if !ok || !isMessageArchievable(msg) {
		return nil
	}
	toJID := msg.ToJID()
	if !m.hosts.IsLocalHost(toJID.Domain()) {
		return nil
	}
	rss, err := m.resMng.GetResources(ctx, toJID.Node())
	if err != nil {
		return err
	}
	if len(rss) > 0 {
		return nil
	}
	return m.archiveMessage(ctx, msg)
}

func (m *Offline) onC2SPresenceRecv(ctx context.Context, execCtx *hook.ExecutionContext) error {
	inf := execCtx.Info.(*hook.C2SStreamInfo)

	pr := inf.Element.(*stravaganza.Presence)
	toJID := pr.ToJID()
	if toJID.IsFull() || !m.hosts.IsLocalHost(toJID.Domain()) {
		return nil
	}
	if !pr.IsAvailable() || pr.Priority() < 0 {
		return nil
	}
	return m.deliverOfflineMessages(ctx, toJID.Node())
}

func (m *Offline) onUserDeleted(ctx context.Context, execCtx *hook.ExecutionContext) error {
	inf := execCtx.Info.(*hook.UserInfo)

	lock, err := m.locker.AcquireLock(ctx, offlineQueueLockID(inf.Username))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release(ctx) }()

	return m.rep.DeleteOfflineMessages(ctx, inf.Username)
}

func (m *Offline) deliverOfflineMessages(ctx context.Context, username string) error {
	lock, err := m.locker.AcquireLock(ctx, offlineQueueLockID(username))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release(ctx) }()

	ms, err := m.rep.FetchOfflineMessages(ctx, username)
	if err != nil {
		return err
	}
	if len(ms) == 0 {
		// empty queue... we're done here
		return nil
	}
	if err := m.rep.DeleteOfflineMessages(ctx, username); err != nil {
		return err
	}
	// route offline messages
	for _, msg := range ms {
		_, _ = m.router.Route(ctx, msg)
	}
	log.Infow("Delivered offline messages", "queue_size", len(ms), "username", username, "xep", "offline")

	return nil
}

func (m *Offline) archiveMessage(ctx context.Context, msg *stravaganza.Message) error {
	toJID := msg.ToJID()
	username := toJID.Node()

	lock, err := m.locker.AcquireLock(ctx, offlineQueueLockID(username))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release(ctx) }()

	qSize, err := m.rep.CountOfflineMessages(ctx, username)
	if err != nil {
		return err
	}
	if qSize == m.cfg.QueueSize { // offline queue is full
		_, _ = m.router.Route(ctx, xmpputil.MakeErrorStanza(msg, stanzaerror.ServiceUnavailable))
		return hook.ErrStopped // already handled
	}
	// add delay info
	dMsg := xmpputil.MakeDelayMessage(msg, time.Now(), toJID.Domain(), "Offline Storage")

	// enqueue offline message
	if err := m.rep.InsertOfflineMessage(ctx, dMsg, username); err != nil {
		return err
	}
	_, err = m.hk.Run(ctx, hook.OfflineMessageArchived, &hook.ExecutionContext{
		Info: &hook.OfflineInfo{
			Username: username,
			Message:  dMsg,
		},
		Sender: m,
	})
	if err != nil {
		return err
	}
	log.Infow("Archived offline message", "id", msg.Attribute(stravaganza.ID), "username", username, "xep", "offline")

	return hook.ErrStopped // already handled
}

func isMessageArchievable(msg *stravaganza.Message) bool {
	if msg.ChildNamespace("no-store", hintsNamespace) != nil {
		return false
	}
	if msg.ChildNamespace("store", hintsNamespace) != nil {
		return true
	}
	return msg.IsNormal() || (msg.IsChat() && msg.IsMessageWithBody())
}

func offlineQueueLockID(username string) string {
	return fmt.Sprintf("offline:queue:%s", username)
}
