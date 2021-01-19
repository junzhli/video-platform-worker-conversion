package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/junzhli/go-video-converter/config"
	"github.com/junzhli/go-video-converter/internal/db"
	"github.com/junzhli/go-video-converter/internal/mq"
	"github.com/streadway/amqp"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	//"github.com/junzhli/go-video-converter/config"
	"github.com/junzhli/go-video-converter/internal/ffmpeg"
	"log"
)

const Version = "0.0.1"

func main() {
	fmt.Printf("===== Video conversion background scheduler (Version %v) =====\n", Version)

	conf := config.ReadEnv()

	database := db.NewMongo(db.Config{
		Username:      conf.DatabaseUsername,
		Password:      conf.DatabasePassword,
		ServerAddress: conf.DatabaseServerAddress,
		ServerPort:    conf.DatabaseServerPort,
	})
	log.Println("Database connection is ready")
	defer database.Close()

	err, rabbitQ := mq.NewRabbitMQ(mq.Config{
		MessageQueueServerAddress: conf.MessageQueueServerAddress,
	})
	if err != nil {
		log.Fatalf("unable to set up rabbitMq connection: %v\n", err)
	}
	log.Println("Message Queue connection is ready")
	videoDoneNotifier := make(chan DoneMessage, 0)

	_ffmpeg := ffmpeg.NewFFmpeg(ffmpeg.Config{
		Destination: "converted/",
	})
	log.Println("FFmpeg packs initialization has completed")

	ctx, cancelFunc := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	ctx = context.WithValue(ctx, "wg", &wg)
	max, err := strconv.ParseInt(conf.MaxNumberOfWorkers, 10, 32)
	if err != nil {
		log.Fatalf("unable to set max_numbers_of_workers: %v\n", err)
	}
	go start(ctx, rabbitQ, videoDoneNotifier, _ffmpeg, database, max)
	go monitorDoneMessage(ctx, rabbitQ, videoDoneNotifier)

	forever := make(chan bool)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<- sig
		cancelFunc()
		rabbitQ.Close()
		forever <- false
	}()

	log.Printf("%v workers are ready... Press Ctrl + C to terminate\n", conf.MaxNumberOfWorkers)
	<- forever
	wg.Wait()
}

type DoneMessage struct {
	Success bool   `json:"Success"`
	VideoId string `json:"VideoId"`
}

func monitorDoneMessage(ctx context.Context, rq mq.RabbitMQ, doneNotifier <- chan DoneMessage) {
	for {
		select {
		case <- ctx.Done():
			break
		case notified := <- doneNotifier:
			msg, err := json.Marshal(notified)
			if err != nil {
				log.Printf("failed to encode done message: VideoId=%v Success=%v\n",
																notified.VideoId, notified.Success)
			}

			err = rq.PublishMessageVideoDone(msg)
			if err != nil {
				log.Printf("failed to publish done message: VideoId=%v Success=%v\n",
					notified.VideoId, notified.Success)
			}
		default:
		}
	}
}

type threadId struct {
	q chan int
}

func (t *threadId) PushIdentifier(i int)  {
	t.q <- i
}

func (t *threadId) PopIdentifier() int {
	return <- t.q
}

func newThreadId(max int) threadId {
	q := make(chan int, max)
	for i := 1; i <= max; i++ {
		q <- i
	}
	return threadId{
		q: q,
	}
}

func start(ctx context.Context,
	rq mq.RabbitMQ, doneNotifier chan <- DoneMessage, _fmpeg ffmpeg.FFmpeg, database db.Mongo, maxConcurrent int64) {
	wg, _ := ctx.Value("wg").(*sync.WaitGroup)
	wg.Add(1)
	defer wg.Done()

	receiver := rq.ReceiverQueue()
	threads := newThreadId(int(maxConcurrent))
	var threadDispatched sync.WaitGroup

	done := false
	for {
		select {
		case <- ctx.Done():
			done = true
			break
		default:
			for r := range receiver {
				threadDispatched = sync.WaitGroup{}
				req, err := parseRequest(r)
				if err != nil {
					log.Printf("unable to parse json in request: %v\n", err)
				} else {
					processVideo(ctx, req, doneNotifier, database, _fmpeg, threads, &threadDispatched)
				}
				r.Ack(false)
			}
		}
		if done {
			break
		}
	}
	log.Printf("Stopping ProcessVideo...")
	threadDispatched.Wait()
}

func parseRequest(r amqp.Delivery) (mq.RequestMessage, error) {
	var req mq.RequestMessage
	err := json.Unmarshal(r.Body, &req)
	if err != nil {
		return req, err
	}
	return req, err
}

func processVideo(ctx context.Context,
	req mq.RequestMessage, doneNotifier chan <- DoneMessage, database db.Mongo, _fmpeg ffmpeg.FFmpeg, threads threadId, wg *sync.WaitGroup) {
	defer func() {
		if re := recover(); re != nil {
			log.Printf("fatal error occurred on preprocessing objectId: %v | reason: %v\n", req.ObjectId, re)
		}
	}()

	log.Printf("objectId: %v, start processing...\n", req.ObjectId)

	err := retry(func() error {
		return database.UpdateStateTempClipById(req.ObjectId, db.StateQueued, db.StateProcessing)
	}, 5)
	if err != nil {
		log.Printf("unable to update state for objectId: %v ... (skipped) | reason: %v\n", req.ObjectId, err)
		return
	}

	transcodingFuncResult, err := _fmpeg.GetTranscodingFunc(req.Source)
	if err != nil {
		log.Printf("unable to generate transcoding functions for objectId: %v ... (skipped) | reason %v\n",
			req.ObjectId, err)
		return
	}

	go writeResult(ctx, req, doneNotifier, transcodingFuncResult, database)

	for _, f := range transcodingFuncResult.Functions {
		id := threads.PopIdentifier()
		threadLog := log.New(os.Stdout, fmt.Sprintf("[worker-%v] ", id), log.LstdFlags)
		wg.Add(1)
		func() {
			defer func() {
				if re := recover(); re != nil {
					threadLog.Printf("fatal error occurred on processing objectId: %v | reason: %v\n", req.ObjectId, re)
					_ = retry(func() error {
						return database.UpdateStateTempClipById(req.ObjectId, db.StateProcessing, db.StateFailed)
					}, 3)
				}

				threadLog.Printf("objectId: %v, quality: %v conversion finished...\n", req.ObjectId, f.Quality)
				wg.Done()
				threads.PushIdentifier(id)
			}()

			threadLog.Printf("objectId: %v, quality: %v conversion started...\n", req.ObjectId, f.Quality)
			result := f.Func()
			if result.Error != nil {
				var failureResult ffmpeg.ProcessFailure
				failureResult.Stderr = result.Stderr
				failureResult.Stdout = result.Stdout
				failureResult.Error = result.Error
				transcodingFuncResult.Notify(false, failureResult)
			} else {
				var successResult ffmpeg.ProcessSuccess
				successResult.Path = result.Result.Path
				successResult.Quality = result.Result.Quality
				transcodingFuncResult.Notify(true, successResult)
			}
		}()
	}
}

func writeResult(ctx context.Context, req mq.RequestMessage,
	doneNotifier chan <- DoneMessage, transcodingFuncResult *ffmpeg.TranscodingFuncResult, database db.Mongo) {
	wg, _ := ctx.Value("wg").(*sync.WaitGroup)
	wg.Add(1)
	defer wg.Done()

	success := transcodingFuncResult.GetDone(ctx)
	if success {
		err := database.UpdateStateTempClipById(req.ObjectId, db.StateProcessing, db.StateFinished)
		if err != nil {
			log.Printf("unable to update state to finished: objectId: %v", req.ObjectId)
			return
		}

		clips := make([]db.Clip, 0, len(transcodingFuncResult.Functions))

		for _, res := range transcodingFuncResult.Results {
			successResult, _ := res.(ffmpeg.ProcessSuccess)

			clips = append(clips, db.Clip{
				Id: primitive.NewObjectID(),
				Object:     db.Object(successResult.Path),
				Definition: db.Definition(successResult.Quality),
				Thumbnails: nil,
			})
		}

		err = database.PushClipsInVideoById(req.VideoId, clips, transcodingFuncResult.Info.Duration)
		if err != nil {
			log.Printf("unable to push clips to video clip: objectId: %v, reason: %v\n", req.VideoId, err)
		}
		err = database.MarkVideoAvailable(req.VideoId)
		if err != nil {
			log.Printf("unable to mark video as available: objectId: %v, reason: %v\n", req.VideoId, err)
		}
	} else {
		err := database.UpdateStateTempClipById(req.ObjectId, db.StateProcessing, db.StateFailed)
		if err != nil {
			log.Printf("unable to update state to failed: objectId: %v", req.ObjectId)
			return
		}

		for _, res := range transcodingFuncResult.Results {
			if successResult, ok := res.(ffmpeg.ProcessSuccess); ok {
				_ = os.Remove(successResult.Path)
				log.Printf(
					"objectId: %v, source: %v quality: %v deleted...\n",
					req.ObjectId, req.Source, successResult.Quality)
			} else if failureResult, ok := res.(ffmpeg.ProcessFailure); ok {
				reason := fmt.Sprintf("err: %v\n stdout: %v\n stderr: %v\n",
					failureResult.Error, failureResult.Stdout.String(), failureResult.Stderr.String())
				log.Printf("objectId: %v, source: %v quality: %v failed... reason: %v\n",
					req.ObjectId, req.Source, failureResult.Quality, reason)
			} else {
				panic("unsupported result")
			}
		}
	}


	doneNotifier <- DoneMessage{
		Success: success,
		VideoId: req.VideoId,
	}

	log.Printf("objectId: %v, state updated\n",
		req.ObjectId)
}

func retry(f func() error, maxTries int) error {
	retry := 0
	var err error
	for retry < maxTries {
		retry++
		err = f()
		if err == nil {
			break
		}
	}
	return err
}


