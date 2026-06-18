// Package ingest subscribes to the station MQTT topics and persists messages.
package ingest

import (
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/tlugger/wtwlt/server/internal/config"
	"github.com/tlugger/wtwlt/server/internal/model"
	"github.com/tlugger/wtwlt/server/internal/store"
)

const (
	topicReadings  = "wtwlt/station/+/readings"
	topicLightning = "wtwlt/station/+/lightning"
	topicStatus    = "wtwlt/station/+/status"
)

type Ingestor struct {
	store  *store.Store
	client mqtt.Client
}

// New builds an MQTT client wired to persist into the store. Call Start to connect.
func New(cfg config.Config, st *store.Store) *Ingestor {
	ing := &Ingestor{store: st}

	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%s", cfg.MQTTHost, cfg.MQTTPort)).
		SetClientID("wtwlt-server").
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetCleanSession(false)
	if cfg.MQTTUser != "" {
		opts.SetUsername(cfg.MQTTUser).SetPassword(cfg.MQTTPass)
	}
	// (Re)subscribe on every (re)connect so we recover after broker restarts.
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Printf("ingest: connected to broker, subscribing")
		ing.subscribe(c)
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		log.Printf("ingest: connection lost: %v", err)
	})

	ing.client = mqtt.NewClient(opts)
	return ing
}

// Start connects to the broker (retrying in the background until it succeeds).
func (i *Ingestor) Start() error {
	tok := i.client.Connect()
	// Don't block forever; with ConnectRetry the client keeps trying in the bg.
	tok.WaitTimeout(5 * time.Second)
	return tok.Error()
}

// Stop disconnects cleanly.
func (i *Ingestor) Stop() {
	i.client.Disconnect(250)
}

func (i *Ingestor) subscribe(c mqtt.Client) {
	c.Subscribe(topicReadings, 1, i.onReadings)
	c.Subscribe(topicLightning, 1, i.onLightning)
	c.Subscribe(topicStatus, 1, i.onStatus)
}

func (i *Ingestor) onReadings(_ mqtt.Client, m mqtt.Message) {
	recv := time.Now().UTC()
	r, err := model.ParseReading(m.Payload())
	if err != nil {
		log.Printf("ingest: bad readings on %s: %v", m.Topic(), err)
		return
	}
	ts, _ := model.EventTime(r.TS, recv)
	if err := i.store.InsertReading(r, ts, recv); err != nil {
		log.Printf("ingest: store readings: %v", err)
	}
}

func (i *Ingestor) onLightning(_ mqtt.Client, m mqtt.Message) {
	recv := time.Now().UTC()
	l, err := model.ParseLightning(m.Payload())
	if err != nil {
		log.Printf("ingest: bad lightning on %s: %v", m.Topic(), err)
		return
	}
	ts, _ := model.EventTime(l.TS, recv)
	if err := i.store.InsertLightning(l, ts, recv); err != nil {
		log.Printf("ingest: store lightning: %v", err)
	}
}

func (i *Ingestor) onStatus(_ mqtt.Client, m mqtt.Message) {
	recv := time.Now().UTC()
	s, err := model.ParseStatus(m.Payload())
	if err != nil {
		log.Printf("ingest: bad status on %s: %v", m.Topic(), err)
		return
	}
	if err := i.store.UpsertStatus(s, recv); err != nil {
		log.Printf("ingest: store status: %v", err)
	}
}
