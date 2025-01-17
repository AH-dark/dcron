package dcron_test

import (
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libi/dcron"
	"github.com/libi/dcron/cron"
	"github.com/libi/dcron/dlog"
	"github.com/libi/dcron/driver"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const (
	DefaultRedisAddr = "127.0.0.1:6379"
)

type TestJobWithWG struct {
	Name string
	WG   *sync.WaitGroup
	Test *testing.T
	Cnt  *atomic.Int32
}

func (job *TestJobWithWG) Run() {
	job.Test.Logf("jobName=[%s], time=%s, job rest count=%d",
		job.Name,
		time.Now().Format("15:04:05"),
		job.Cnt.Load(),
	)
	if job.Cnt.Load() == 0 {
		return
	} else {
		job.Cnt.Store(job.Cnt.Add(-1))
		if job.Cnt.Load() == 0 {
			job.WG.Done()
		}
	}
}

func TestMultiNodes(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(3)
	testJobWGs := make([]*sync.WaitGroup, 0)
	testJobWGs = append(testJobWGs, &sync.WaitGroup{})
	testJobWGs = append(testJobWGs, &sync.WaitGroup{})
	testJobWGs = append(testJobWGs, &sync.WaitGroup{})
	testJobWGs[0].Add(1)
	testJobWGs[1].Add(1)

	testJobs := make([]*TestJobWithWG, 0)
	testJobs = append(
		testJobs,
		&TestJobWithWG{
			Name: "s1_test1",
			WG:   testJobWGs[0],
			Test: t,
			Cnt:  &atomic.Int32{},
		},
		&TestJobWithWG{
			Name: "s1_test2",
			WG:   testJobWGs[1],
			Test: t,
			Cnt:  &atomic.Int32{},
		},
		&TestJobWithWG{
			Name: "s1_test3",
			WG:   testJobWGs[2],
			Test: t,
			Cnt:  &atomic.Int32{},
		})
	testJobs[0].Cnt.Store(5)
	testJobs[1].Cnt.Store(5)

	nodeCancel := make([](chan int), 3)
	nodeCancel[0] = make(chan int, 1)
	nodeCancel[1] = make(chan int, 1)
	nodeCancel[2] = make(chan int, 1)

	// 间隔1秒启动测试节点刷新逻辑
	go runNode(t, wg, testJobs, nodeCancel[0])
	<-time.After(time.Second)
	go runNode(t, wg, testJobs, nodeCancel[1])
	<-time.After(time.Second)
	go runNode(t, wg, testJobs, nodeCancel[2])

	testJobWGs[0].Wait()
	testJobWGs[1].Wait()

	close(nodeCancel[0])
	close(nodeCancel[1])
	close(nodeCancel[2])

	wg.Wait()
}

func runNode(t *testing.T, wg *sync.WaitGroup, testJobs []*TestJobWithWG, cancel chan int) {
	redisCli := redis.NewClient(&redis.Options{
		Addr: DefaultRedisAddr,
	})
	drv := driver.NewRedisDriver(redisCli)
	dcron := dcron.NewDcronWithOption(
		t.Name(),
		drv,
		dcron.WithLogger(
			dlog.DefaultPrintfLogger(
				log.New(os.Stdout, "", log.LstdFlags))))
	// 添加多个任务 启动多个节点时 任务会均匀分配给各个节点

	var err error
	for _, job := range testJobs {
		if err = dcron.AddJob(job.Name, "* * * * *", job); err != nil {
			t.Error("add job error")
		}
	}

	dcron.Start()
	//移除测试
	dcron.Remove(testJobs[2].Name)
	<-cancel
	dcron.Stop()
	wg.Done()
}

func Test_SecondsJob(t *testing.T) {
	redisCli := redis.NewClient(&redis.Options{
		Addr: DefaultRedisAddr,
	})
	drv := driver.NewRedisDriver(redisCli)
	dcr := dcron.NewDcronWithOption(t.Name(), drv, dcron.CronOptionSeconds())
	err := dcr.AddFunc("job1", "*/5 * * * * *", func() {
		t.Log(time.Now())
	})
	if err != nil {
		t.Error(err)
	}
	dcr.Start()
	time.Sleep(15 * time.Second)
	dcr.Stop()
}

func runSecondNode(id string, wg *sync.WaitGroup, runningTime time.Duration, t *testing.T) {
	redisCli := redis.NewClient(&redis.Options{
		Addr: DefaultRedisAddr,
	})
	drv := driver.NewRedisDriver(redisCli)
	dcr := dcron.NewDcronWithOption(t.Name(), drv,
		dcron.CronOptionSeconds(),
		dcron.WithLogger(dlog.DefaultPrintfLogger(
			log.New(os.Stdout, "["+id+"]", log.LstdFlags),
		)),
		dcron.CronOptionChain(cron.Recover(
			cron.DefaultLogger,
		)),
	)
	var err error
	err = dcr.AddFunc("job1", "*/5 * * * * *", func() {
		t.Log(time.Now())
	})
	require.Nil(t, err)
	err = dcr.AddFunc("job2", "*/8 * * * * *", func() {
		panic("test panic")
	})
	require.Nil(t, err)
	err = dcr.AddFunc("job3", "*/2 * * * * *", func() {
		t.Log("job3:", time.Now())
	})
	require.Nil(t, err)
	dcr.Start()
	<-time.After(runningTime)
	dcr.Stop()
	wg.Done()
}

func runSecondNodeWithLogger(id string, wg *sync.WaitGroup, runningTime time.Duration, t *testing.T) {
	redisCli := redis.NewClient(&redis.Options{
		Addr: DefaultRedisAddr,
	})
	drv := driver.NewRedisDriver(redisCli)
	dcr := dcron.NewDcronWithOption(
		t.Name(),
		drv,
		dcron.CronOptionSeconds(),
		dcron.WithLogger(dlog.VerbosePrintfLogger(
			log.New(os.Stdout, "["+id+"]", log.LstdFlags),
		)),
		dcron.CronOptionChain(cron.Recover(
			cron.DefaultLogger,
		)),
	)
	var err error
	err = dcr.AddFunc("job1", "*/5 * * * * *", func() {
		t.Log(time.Now())
	})
	require.Nil(t, err)
	err = dcr.AddFunc("job2", "*/8 * * * * *", func() {
		panic("test panic")
	})
	require.Nil(t, err)
	err = dcr.AddFunc("job3", "*/2 * * * * *", func() {
		t.Log("job3:", time.Now())
	})
	require.Nil(t, err)
	dcr.Start()
	<-time.After(runningTime)
	dcr.Stop()
	wg.Done()
}

func Test_SecondJobWithPanicAndMultiNodes(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(5)
	go runSecondNode("1", wg, 45*time.Second, t)
	go runSecondNode("2", wg, 45*time.Second, t)
	go runSecondNode("3", wg, 45*time.Second, t)
	go runSecondNode("4", wg, 45*time.Second, t)
	go runSecondNode("5", wg, 45*time.Second, t)
	wg.Wait()
}

func Test_SecondJobWithStopAndSwapNode(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(2)
	go runSecondNode("1", wg, 60*time.Second, t)
	go runSecondNode("2", wg, 20*time.Second, t)
	wg.Wait()
}

func Test_WithClusterStableNodes(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(5)

	runningTime := 60 * time.Second
	startFunc := func(id string, timeWindow time.Duration, t *testing.T) {
		redisCli := redis.NewClient(&redis.Options{
			Addr: DefaultRedisAddr,
		})
		drv := driver.NewRedisDriver(redisCli)
		dcr := dcron.NewDcronWithOption(t.Name(), drv,
			dcron.CronOptionSeconds(),
			dcron.WithLogger(dlog.DefaultPrintfLogger(
				log.New(os.Stdout, "["+id+"]", log.LstdFlags)),
			),
			dcron.WithClusterStable(timeWindow),
			dcron.WithNodeUpdateDuration(timeWindow),
		)
		var err error
		err = dcr.AddFunc("job1", "*/3 * * * * *", func() {
			t.Log(time.Now())
		})
		require.Nil(t, err)
		err = dcr.AddFunc("job2", "*/8 * * * * *", func() {
			t.Logf("job2: %v", time.Now())
		})
		require.Nil(t, err)
		err = dcr.AddFunc("job3", "* * * * * *", func() {
			t.Log("job3:", time.Now())
		})
		require.Nil(t, err)
		dcr.Start()
		<-time.After(runningTime)
		dcr.Stop()
		wg.Done()
	}

	go startFunc("1", time.Second*6, t)
	go startFunc("2", time.Second*6, t)
	go startFunc("3", time.Second*6, t)
	go startFunc("4", time.Second*6, t)
	go startFunc("5", time.Second*6, t)
}

func Test_SecondJobLog_Issue68(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(5)
	go runSecondNodeWithLogger("1", wg, 45*time.Second, t)
	go runSecondNodeWithLogger("2", wg, 45*time.Second, t)
	go runSecondNodeWithLogger("3", wg, 45*time.Second, t)
	go runSecondNodeWithLogger("4", wg, 45*time.Second, t)
	go runSecondNodeWithLogger("5", wg, 45*time.Second, t)
	wg.Wait()
}
