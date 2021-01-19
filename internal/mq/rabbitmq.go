package mq

import (
	"github.com/streadway/amqp"
)

const (
	queueNameVideoConversion = "video_conversion"
	queueNameVideoDone = "video_done"
)

// RabbitMQ wraps amqp protocol based library with internal-facing queueVideoConversion set up
type RabbitMQ interface {
	// Close to stop the connection to queueVideoConversion server (rabbitmq)
	Close() error
	// ReceiverQueue receives requests emitted by queueVideoConversion server
	ReceiverQueue() <- chan amqp.Delivery
	// PublishMessageVideoDone publish message to queue, signaling subscribers that video conversion's result is out
	PublishMessageVideoDone(msg []byte) error
}

type RequestMessage struct {
	Source string `json:"source"`
	ObjectId string `json:"object_id"`
	VideoId string `json:"video_id"`
}

type rabbitMq struct {
	connection           *amqp.Connection
	channel              *amqp.Channel
	queueVideoConversion amqp.Queue
	queueVideoDone 		 amqp.Queue
	receiverQueue        <- chan amqp.Delivery
}

func (r* rabbitMq) ReceiverQueue() <-chan amqp.Delivery {
	return r.receiverQueue
}

func (r* rabbitMq) Close() error {
	return r.connection.Close()
}

func (r* rabbitMq) PublishMessageVideoDone(msg []byte) error {
	return r.channel.Publish(
		"",
		queueNameVideoDone,
		false,
		false,
		amqp.Publishing{
			ContentType: "text/plain",
			Body: msg,
		})
}

type Config struct {
	MessageQueueServerAddress string
}

func NewRabbitMQ(c Config) (error, RabbitMQ)  {
	connection, err := amqp.Dial("amqp://" + c.MessageQueueServerAddress)
	if err != nil {
		return err, nil
	}

	channel, err := connection.Channel()
	if err != nil {
		return err, nil
	}

	queueVideoConversion, err := channel.QueueDeclare(
		queueNameVideoConversion,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err, nil
	}

	receiverQueue, err := channel.Consume(
		queueVideoConversion.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err, nil
	}

	queueVideoDone, err := channel.QueueDeclare(
		queueNameVideoDone,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err, nil
	}

	var rabbitMq rabbitMq
	rabbitMq.channel = channel
	rabbitMq.connection = connection
	rabbitMq.queueVideoConversion = queueVideoConversion
	rabbitMq.queueVideoDone = queueVideoDone
	rabbitMq.receiverQueue = receiverQueue

	return nil, &rabbitMq
}