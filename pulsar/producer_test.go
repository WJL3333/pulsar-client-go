// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package pulsar

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/apache/pulsar-client-go/pulsar/internal"
	"github.com/apache/pulsar-client-go/pulsar/internal/pb"
	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"
)

func TestInvalidURL(t *testing.T) {
	client, err := NewClient(ClientOptions{})

	if client != nil || err == nil {
		t.Fatal("Should have failed to create client")
	}
}

func TestProducerConnectError(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: "pulsar://invalid-hostname:6650",
	})

	assert.Nil(t, err)

	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic: newTopicName(),
	})

	// Expect error in creating producer
	assert.Nil(t, producer)
	assert.NotNil(t, err)

	assert.Equal(t, err.Error(), "connection error")
}

func TestProducerNoTopic(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: "pulsar://localhost:6650",
	})

	if err != nil {
		t.Fatal(err)
		return
	}

	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{})

	// Expect error in creating producer
	assert.Nil(t, producer)
	assert.NotNil(t, err)

	assert.Equal(t, err.(*Error).Result(), ResultInvalidTopicName)
}

func TestSimpleProducer(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.NoError(t, err)
	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic: newTopicName(),
	})

	assert.NoError(t, err)
	assert.NotNil(t, producer)
	defer producer.Close()

	for i := 0; i < 10; i++ {
		ID, err := producer.Send(context.Background(), &ProducerMessage{
			Payload: []byte("hello"),
		})

		assert.NoError(t, err)
		assert.NotNil(t, ID)
	}
}

func TestProducerAsyncSend(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.NoError(t, err)
	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic:                   newTopicName(),
		BatchingMaxPublishDelay: 1 * time.Second,
	})

	assert.NoError(t, err)
	assert.NotNil(t, producer)
	defer producer.Close()

	wg := sync.WaitGroup{}
	wg.Add(10)
	errors := internal.NewBlockingQueue(10)

	for i := 0; i < 10; i++ {
		producer.SendAsync(context.Background(), &ProducerMessage{
			Payload: []byte("hello"),
		}, func(id MessageID, message *ProducerMessage, e error) {
			if e != nil {
				log.WithError(e).Error("Failed to publish")
				errors.Put(e)
			} else {
				log.Info("Published message ", id)
			}
			wg.Done()
		})

		assert.NoError(t, err)
	}

	err = producer.Flush()
	assert.Nil(t, err)

	wg.Wait()

	assert.Equal(t, 0, errors.Size())
}

func TestProducerCompression(t *testing.T) {

	type testProvider struct {
		name            string
		compressionType CompressionType
	}

	var providers = []testProvider{
		{"zlib", ZLib},
		{"lz4", LZ4},
		{"zstd", ZSTD},
	}

	for _, provider := range providers {
		p := provider
		t.Run(p.name, func(t *testing.T) {
			client, err := NewClient(ClientOptions{
				URL: serviceURL,
			})
			assert.NoError(t, err)
			defer client.Close()

			producer, err := client.CreateProducer(ProducerOptions{
				Topic:           newTopicName(),
				CompressionType: p.compressionType,
			})

			assert.NoError(t, err)
			assert.NotNil(t, producer)
			defer producer.Close()

			for i := 0; i < 10; i++ {
				ID, err := producer.Send(context.Background(), &ProducerMessage{
					Payload: []byte("hello"),
				})

				assert.NoError(t, err)
				assert.NotNil(t, ID)
			}
		})
	}
}

func TestProducerLastSequenceID(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.NoError(t, err)
	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic: newTopicName(),
	})

	assert.NoError(t, err)
	assert.NotNil(t, producer)
	defer producer.Close()

	assert.Equal(t, int64(-1), producer.LastSequenceID())

	for i := 0; i < 10; i++ {
		ID, err := producer.Send(context.Background(), &ProducerMessage{
			Payload: []byte("hello"),
		})

		assert.NoError(t, err)
		assert.NotNil(t, ID)
		assert.Equal(t, int64(i), producer.LastSequenceID())
	}
}

func TestEventTime(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.NoError(t, err)
	defer client.Close()

	topicName := "test-event-time"
	producer, err := client.CreateProducer(ProducerOptions{
		Topic: topicName,
	})
	assert.Nil(t, err)
	defer producer.Close()

	consumer, err := client.Subscribe(ConsumerOptions{
		Topic:            topicName,
		SubscriptionName: "subName",
	})
	assert.Nil(t, err)
	defer consumer.Close()

	eventTime := timeFromUnixTimestampMillis(uint64(1565161612))
	ID, err := producer.Send(context.Background(), &ProducerMessage{
		Payload:   []byte(fmt.Sprintf("test-event-time")),
		EventTime: eventTime,
	})
	assert.Nil(t, err)
	assert.NotNil(t, ID)

	msg, err := consumer.Receive(context.Background())
	assert.Nil(t, err)
	actualEventTime := msg.EventTime()
	assert.Equal(t, eventTime.Unix(), actualEventTime.Unix())
}

func TestFlushInProducer(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.NoError(t, err)
	defer client.Close()

	topicName := "test-flush-in-producer"
	subName := "subscription-name"
	numOfMessages := 10
	ctx := context.Background()

	// set batch message number numOfMessages, and max delay 10s
	producer, err := client.CreateProducer(ProducerOptions{
		Topic:                   topicName,
		DisableBatching:         false,
		BatchingMaxMessages:     uint(numOfMessages),
		BatchingMaxPublishDelay: time.Second * 10,
		Properties: map[string]string{
			"producer-name": "test-producer-name",
			"producer-id":   "test-producer-id",
		},
	})
	assert.Nil(t, err)
	defer producer.Close()

	consumer, err := client.Subscribe(ConsumerOptions{
		Topic:            topicName,
		SubscriptionName: subName,
	})
	assert.Nil(t, err)
	defer consumer.Close()

	prefix := "msg-batch-async"
	msgCount := 0

	wg := sync.WaitGroup{}
	wg.Add(5)
	errors := internal.NewBlockingQueue(10)
	for i := 0; i < numOfMessages/2; i++ {
		messageContent := prefix + fmt.Sprintf("%d", i)
		producer.SendAsync(ctx, &ProducerMessage{
			Payload: []byte(messageContent),
		}, func(id MessageID, producerMessage *ProducerMessage, e error) {
			if e != nil {
				log.WithError(e).Error("Failed to publish")
				errors.Put(e)
			} else {
				log.Info("Published message ", id)
			}
			wg.Done()
		})
		assert.Nil(t, err)
	}
	err = producer.Flush()
	assert.Nil(t, err)
	wg.Wait()

	var ledgerID int64 = -1
	var entryID int64 = -1

	for i := 0; i < numOfMessages/2; i++ {
		msg, err := consumer.Receive(ctx)
		assert.Nil(t, err)
		msgCount++

		msgID := msg.ID().(*messageID)
		// Since messages are batched, they will be sharing the same ledgerId/entryId
		if ledgerID == -1 {
			ledgerID = msgID.ledgerID
			entryID = msgID.entryID
		} else {
			assert.Equal(t, ledgerID, msgID.ledgerID)
			assert.Equal(t, entryID, msgID.entryID)
		}
	}

	assert.Equal(t, msgCount, numOfMessages/2)

	wg.Add(5)
	for i := numOfMessages / 2; i < numOfMessages; i++ {
		messageContent := prefix + fmt.Sprintf("%d", i)
		producer.SendAsync(ctx, &ProducerMessage{
			Payload: []byte(messageContent),
		}, func(id MessageID, producerMessage *ProducerMessage, e error) {
			if e != nil {
				log.WithError(e).Error("Failed to publish")
				errors.Put(e)
			} else {
				log.Info("Published message ", id)
			}
			wg.Done()
		})
		assert.Nil(t, err)
	}

	err = producer.Flush()
	assert.Nil(t, err)
	wg.Wait()

	for i := numOfMessages / 2; i < numOfMessages; i++ {
		_, err := consumer.Receive(ctx)
		assert.Nil(t, err)
		msgCount++
	}
	assert.Equal(t, msgCount, numOfMessages)
}

func TestFlushInPartitionedProducer(t *testing.T) {
	topicName := "public/default/partition-testFlushInPartitionedProducer"

	// call admin api to make it partitioned
	url := adminURL + "/" + "admin/v2/persistent/" + topicName + "/partitions"
	makeHTTPCall(t, http.MethodPut, url, "5")

	numberOfPartitions := 5
	numOfMessages := 10
	ctx := context.Background()

	// creat client connection
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.NoError(t, err)
	defer client.Close()

	// create consumer
	consumer, err := client.Subscribe(ConsumerOptions{
		Topic:            topicName,
		SubscriptionName: "my-sub",
		Type:             Exclusive,
	})
	assert.Nil(t, err)
	defer consumer.Close()

	// create producer and set batch message number numOfMessages, and max delay 10s
	producer, err := client.CreateProducer(ProducerOptions{
		Topic:                   topicName,
		DisableBatching:         false,
		BatchingMaxMessages:     uint(numOfMessages / numberOfPartitions),
		BatchingMaxPublishDelay: time.Second * 10,
	})
	assert.Nil(t, err)
	defer producer.Close()

	// send 5 messages
	prefix := "msg-batch-async-"
	wg := sync.WaitGroup{}
	wg.Add(5)
	errors := internal.NewBlockingQueue(5)
	for i := 0; i < numOfMessages/2; i++ {
		messageContent := prefix + fmt.Sprintf("%d", i)
		producer.SendAsync(ctx, &ProducerMessage{
			Payload: []byte(messageContent),
		}, func(id MessageID, producerMessage *ProducerMessage, e error) {
			if e != nil {
				log.WithError(e).Error("Failed to publish")
				errors.Put(e)
			} else {
				log.Info("Published message: ", id)
			}
			wg.Done()
		})
		assert.Nil(t, err)
	}

	// After flush, should be able to consume.
	err = producer.Flush()
	assert.Nil(t, err)

	wg.Wait()

	// Receive all messages
	msgCount := 0
	for i := 0; i < numOfMessages/2; i++ {
		msg, err := consumer.Receive(ctx)
		fmt.Printf("Received message msgId: %#v -- content: '%s'\n",
			msg.ID(), string(msg.Payload()))
		assert.Nil(t, err)
		consumer.Ack(msg)
		msgCount++
	}
	assert.Equal(t, msgCount, numOfMessages/2)
}

func TestRoundRobinRouterPartitionedProducer(t *testing.T) {
	topicName := "public/default/partition-testRoundRobinRouterPartitionedProducer"
	numberOfPartitions := 5

	// call admin api to make it partitioned
	url := adminURL + "/" + "admin/v2/persistent/" + topicName + "/partitions"
	makeHTTPCall(t, http.MethodPut, url, strconv.Itoa(numberOfPartitions))

	numOfMessages := 10
	ctx := context.Background()

	// creat client connection
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.NoError(t, err)
	defer client.Close()

	// create consumer
	consumer, err := client.Subscribe(ConsumerOptions{
		Topic:            topicName,
		SubscriptionName: "my-sub",
		Type:             Exclusive,
	})
	assert.Nil(t, err)
	defer consumer.Close()

	// create producer
	producer, err := client.CreateProducer(ProducerOptions{
		Topic:           topicName,
		DisableBatching: true,
	})
	assert.Nil(t, err)
	defer producer.Close()

	// send 5 messages
	prefix := "msg-"

	for i := 0; i < numOfMessages; i++ {
		messageContent := prefix + fmt.Sprintf("%d", i)
		_, err = producer.Send(ctx, &ProducerMessage{
			Payload: []byte(messageContent),
		})
		assert.Nil(t, err)
	}

	// Receive all messages
	msgCount := 0
	msgPartitionMap := make(map[string]int)
	for i := 0; i < numOfMessages; i++ {
		msg, err := consumer.Receive(ctx)
		fmt.Printf("Received message msgId: %#v topic: %s-- content: '%s'\n",
			msg.ID(), msg.Topic(), string(msg.Payload()))
		assert.Nil(t, err)
		consumer.Ack(msg)
		msgCount++
		msgPartitionMap[msg.Topic()]++
	}
	assert.Equal(t, msgCount, numOfMessages)
	assert.Equal(t, numberOfPartitions, len(msgPartitionMap))
	for _, count := range msgPartitionMap {
		assert.Equal(t, count, numOfMessages/numberOfPartitions)
	}
}

func TestMessageRouter(t *testing.T) {
	// Create topic with 5 partitions
	err := httpPut("admin/v2/persistent/public/default/my-partitioned-topic/partitions", 5)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})

	assert.Nil(t, err)
	defer client.Close()

	// Only subscribe on the specific partition
	consumer, err := client.Subscribe(ConsumerOptions{
		Topic:            "my-partitioned-topic-partition-2",
		SubscriptionName: "my-sub",
	})

	assert.Nil(t, err)
	defer consumer.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic: "my-partitioned-topic",
		MessageRouter: func(msg *ProducerMessage, tm TopicMetadata) int {
			fmt.Println("Routing message ", msg, " -- Partitions: ", tm.NumPartitions())
			return 2
		},
	})

	assert.Nil(t, err)
	defer producer.Close()

	ctx := context.Background()

	ID, err := producer.Send(ctx, &ProducerMessage{
		Payload: []byte("hello"),
	})
	assert.Nil(t, err)
	assert.NotNil(t, ID)

	fmt.Println("PUBLISHED")

	// Verify message was published on partition 2
	msg, err := consumer.Receive(ctx)
	assert.Nil(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, string(msg.Payload()), "hello")
}

func TestNonPersistentTopic(t *testing.T) {
	topicName := "non-persistent://public/default/testNonPersistentTopic"
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.Nil(t, err)
	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic: topicName,
	})

	assert.Nil(t, err)
	defer producer.Close()

	consumer, err := client.Subscribe(ConsumerOptions{
		Topic:            topicName,
		SubscriptionName: "my-sub",
	})
	assert.Nil(t, err)
	defer consumer.Close()
}

func TestProducerDuplicateNameOnSameTopic(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	topicName := newTopicName()
	producerName := "my-producer"

	p1, err := client.CreateProducer(ProducerOptions{
		Topic: topicName,
		Name:  producerName,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p1.Close()

	_, err = client.CreateProducer(ProducerOptions{
		Topic: topicName,
		Name:  producerName,
	})
	assert.NotNil(t, err, "expected error when creating producer with same name")
}

func TestProducerMetadata(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: lookupURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	topic := newTopicName()
	props := map[string]string{
		"key1": "value1",
	}
	producer, err := client.CreateProducer(ProducerOptions{
		Topic:      topic,
		Name:       "my-producer",
		Properties: props,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	stats, err := topicStats(topic)
	if err != nil {
		t.Fatal(err)
	}

	meta := stats["publishers"].([]interface{})[0].(map[string]interface{})["metadata"].(map[string]interface{})
	assert.Equal(t, len(props), len(meta))
	for k, v := range props {
		mv := meta[k].(string)
		assert.Equal(t, v, mv)
	}
}

// test for issues #76, #114 and #123
func TestBatchMessageFlushing(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: lookupURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	topic := newTopicName()
	producer, err := client.CreateProducer(ProducerOptions{
		Topic: topic,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()

	maxBytes := internal.MaxBatchSize
	genbytes := func(n int) []byte {
		c := []byte("a")[0]
		bytes := make([]byte, n)
		for i := 0; i < n; i++ {
			bytes[i] = c
		}
		return bytes
	}

	msgs := [][]byte{
		genbytes(maxBytes - 10),
		genbytes(11),
	}

	ch := make(chan struct{}, 2)
	ctx := context.Background()
	for _, msg := range msgs {
		msg := &ProducerMessage{
			Payload: msg,
		}
		producer.SendAsync(ctx, msg, func(id MessageID, producerMessage *ProducerMessage, err error) {
			ch <- struct{}{}
		})
	}

	published := 0
	keepGoing := true
	for keepGoing {
		select {
		case <-ch:
			published++
			if published == 2 {
				keepGoing = false
			}
		case <-time.After(defaultBatchingMaxPublishDelay * 10):
			fmt.Println("TestBatchMessageFlushing timeout waiting to publish messages")
			keepGoing = false
		}
	}

	assert.Equal(t, 2, published, "expected to publish two messages")
}

func TestDelayRelative(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.NoError(t, err)
	defer client.Close()

	topicName := newTopicName()
	producer, err := client.CreateProducer(ProducerOptions{
		Topic: topicName,
	})
	assert.Nil(t, err)
	defer producer.Close()

	consumer, err := client.Subscribe(ConsumerOptions{
		Topic:            topicName,
		SubscriptionName: "subName",
		Type:             Shared,
	})
	assert.Nil(t, err)
	defer consumer.Close()

	ID, err := producer.Send(context.Background(), &ProducerMessage{
		Payload:      []byte(fmt.Sprintf("test")),
		DeliverAfter: 3 * time.Second,
	})
	assert.Nil(t, err)
	assert.NotNil(t, ID)

	ctx, canc := context.WithTimeout(context.Background(), 1*time.Second)

	msg, err := consumer.Receive(ctx)
	assert.Error(t, err)
	assert.Nil(t, msg)
	canc()

	ctx, canc = context.WithTimeout(context.Background(), 5*time.Second)
	msg, err = consumer.Receive(ctx)
	assert.Nil(t, err)
	assert.NotNil(t, msg)
	canc()
}

func TestDelayAbsolute(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: serviceURL,
	})
	assert.NoError(t, err)
	defer client.Close()

	topicName := newTopicName()
	producer, err := client.CreateProducer(ProducerOptions{
		Topic: topicName,
	})
	assert.Nil(t, err)
	defer producer.Close()

	consumer, err := client.Subscribe(ConsumerOptions{
		Topic:            topicName,
		SubscriptionName: "subName",
		Type:             Shared,
	})
	assert.Nil(t, err)
	defer consumer.Close()

	ID, err := producer.Send(context.Background(), &ProducerMessage{
		Payload:   []byte(fmt.Sprintf("test")),
		DeliverAt: time.Now().Add(3 * time.Second),
	})
	assert.Nil(t, err)
	assert.NotNil(t, ID)

	ctx, canc := context.WithTimeout(context.Background(), 1*time.Second)

	msg, err := consumer.Receive(ctx)
	assert.Error(t, err)
	assert.Nil(t, msg)
	canc()

	ctx, canc = context.WithTimeout(context.Background(), 5*time.Second)
	msg, err = consumer.Receive(ctx)
	assert.Nil(t, err)
	assert.NotNil(t, msg)
	canc()
}
func TestProducerSendAsyncTimeoutBecauseTooLongBatchingMaxPublishDelay(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: "pulsar://localhost:6650",
	})

	if err != nil {
		log.Fatal(err)
	}

	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic:                   "topic-1",
		BatchingMaxPublishDelay: time.Second * 3,
	})

	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	var SendTimeout = time.Millisecond * 30
	for i := 0; i < 1000; i++ {
		timeoutCtx, cancel := context.WithTimeout(ctx, SendTimeout)
		producer.SendAsync(timeoutCtx, &ProducerMessage{
			Payload: []byte(fmt.Sprintf("hello-%d", i)),
		}, func(msgId MessageID, message *ProducerMessage, e error) {
			assert.Equal(t, context.DeadlineExceeded, e)
			assert.Nil(t, msgId)
			cancel()
		})
	}

	time.Sleep(4 * time.Second)
	producer.Close()
}

func TestProducerSendAsyncTimeoutBecauseResponseTooLate(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: "pulsar://localhost:6650",
	})

	if err != nil {
		log.Fatal(err)
	}

	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic: "topic-1",
		beforeReceiveResponseCallback: func(receipt *pb.CommandSendReceipt) {
			time.Sleep(150 * time.Millisecond)
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	var SendTimeout = time.Millisecond * 90

	for times := 0; times < 5; times++ {
		for i := 0; i < 1000; i++ {
			startTime := time.Now()
			timeoutCtx, cancel := context.WithTimeout(ctx, SendTimeout)
			producer.SendAsync(timeoutCtx, &ProducerMessage{
				Payload: []byte(fmt.Sprintf("hello-%d", i)),
			}, func(msgId MessageID, message *ProducerMessage, e error) {
				endTime := time.Now()
				useTime := endTime.Sub(startTime)
				assert.True(t, useTime < SendTimeout*2, fmt.Sprintf("sendTimeout set %v. useTime %v", SendTimeout, useTime))
				assert.Equal(t, context.DeadlineExceeded, e)
				assert.Nil(t, msgId)
				cancel()
			})
		}
		producer.Flush()
	}

	time.Sleep(1 * time.Second)
	producer.Close()
}

func TestProducerSendAsyncTimeoutNormalResponseTime(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: "pulsar://localhost:6650",
	})

	if err != nil {
		log.Fatal(err)
	}

	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic: "topic-1",
	})

	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	var SendTimeout = time.Millisecond * 100

	for i := 0; i < 1000; i++ {
		timeoutCtx, cancel := context.WithTimeout(ctx, SendTimeout)
		startTime := time.Now()
		producer.SendAsync(timeoutCtx, &ProducerMessage{
			Payload: []byte(fmt.Sprintf("hello-%d", i)),
		}, func(msgId MessageID, message *ProducerMessage, e error) {
			endTime := time.Now()
			useTime := endTime.Sub(startTime)
			assert.True(t, useTime < SendTimeout*2, fmt.Sprintf("sendTimeout set %v. useTime %v", SendTimeout, useTime))
			assert.Nil(t, e)
			assert.NotNil(t, msgId)
			cancel()
		})
	}
	producer.Flush()

	time.Sleep(1 * time.Second)
	producer.Close()
}

// the context is only start check after the message has flushed
// or before add to BatchBuilder
// if you set too big BatchingMaxPublishDelay
// callBack will be returned in BatchingMaxPublishDelay if you cancel the context.
func TestProducerSendAsyncTimeoutEarlyCancel(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: "pulsar://localhost:6650",
	})

	if err != nil {
		log.Fatal(err)
	}

	defer client.Close()

	var SendTimeout = time.Millisecond * 90
	var BatchingMaxPublishDelay = time.Second * 1

	producer, err := client.CreateProducer(ProducerOptions{
		Topic:                   "topic-1",
		BatchingMaxPublishDelay: BatchingMaxPublishDelay,
		beforeReceiveResponseCallback: func(receipt *pb.CommandSendReceipt) {
			time.Sleep(3 * time.Second)
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	var estimatedUpperBoundSendTimeout time.Duration

	if SendTimeout < BatchingMaxPublishDelay {
		estimatedUpperBoundSendTimeout = BatchingMaxPublishDelay + BatchingMaxPublishDelay/3
	} else {
		estimatedUpperBoundSendTimeout = SendTimeout + SendTimeout/3
	}

	for times := 0; times < 5; times++ {

		var cancels = make([]context.CancelFunc, 0)

		for i := 0; i < 1000; i++ {
			msgNumber := i
			timeoutCtx, cancel := context.WithTimeout(ctx, SendTimeout)
			startTime := time.Now()
			cancels = append(cancels, cancel)
			producer.SendAsync(timeoutCtx, &ProducerMessage{
				Payload: []byte(fmt.Sprintf("hello-%d", i)),
			}, func(msgId MessageID, message *ProducerMessage, e error) {
				endTime := time.Now()
				useTime := endTime.Sub(startTime)
				assert.True(t, useTime < estimatedUpperBoundSendTimeout, fmt.Sprintf("message %v sendTimeout set %v. useTime %v", msgNumber, SendTimeout, useTime))
				//assert.Equal(t, context.Canceled, e)
				assert.Nil(t, msgId)
			})
		}
		time.Sleep(10 * time.Millisecond)
		for _, cancel := range cancels {
			cancel()
		}
	}
	producer.Close()
}

func TestProducerSendTimeout(t *testing.T) {
	client, err := NewClient(ClientOptions{
		URL: "pulsar://localhost:6650",
	})

	if err != nil {
		log.Fatal(err)
	}

	defer client.Close()

	producer, err := client.CreateProducer(ProducerOptions{
		Topic: "topic-1",
		beforeReceiveResponseCallback: func(receipt *pb.CommandSendReceipt) {
			time.Sleep(1 * time.Millisecond * 100)
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	var SendTimeout = time.Millisecond * 60
	var BatchingMaxPublishDelay = time.Millisecond * 10

	var estimatedUpperBoundSendTimeout time.Duration

	if SendTimeout < BatchingMaxPublishDelay {
		estimatedUpperBoundSendTimeout = BatchingMaxPublishDelay + BatchingMaxPublishDelay/3
	} else {
		estimatedUpperBoundSendTimeout = SendTimeout + SendTimeout/3
	}

	for times := 0; times < 3; times++ {
		for i := 0; i < 100; i++ {
			timeoutCtx, cancel := context.WithTimeout(ctx, SendTimeout)
			startTime := time.Now()
			msgID, err := producer.Send(timeoutCtx, &ProducerMessage{
				Payload: []byte(fmt.Sprintf("hello-%d", i)),
			})
			endTime := time.Now()
			useTime := endTime.Sub(startTime)
			assert.True(t, useTime < estimatedUpperBoundSendTimeout, fmt.Sprintf("messageNum %v sendTimeout set %v. useTime %v", i, SendTimeout, useTime))
			assert.Equal(t, context.DeadlineExceeded, err)
			assert.Nil(t, msgID)
			cancel()
		}
	}

	time.Sleep(1 * time.Second)
	producer.Close()
}
