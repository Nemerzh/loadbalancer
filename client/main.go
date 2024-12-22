package main

import (
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

func main() {
	c := http.Client{
		Timeout: 60 * time.Second,
	}

	wg := sync.WaitGroup{}

	for j := 0; j < 5; j++ {

		wg.Add(1)
		go func(h int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				time.Sleep(100 * time.Millisecond)

				req, err := http.NewRequest("GET", "http://127.0.0.1:8100/api/", nil)
				if err != nil {
					log.Println(err)

					continue
				}

				req.Header.Set("Accept", "text/html")

				resp, err := c.Do(req)
				if err != nil {
					log.Printf("got %d err: %s", i, err)

					continue
				}

				b, err := io.ReadAll(resp.Body)
				if err != nil {
					log.Printf("got %d err: %s", i, err)

					continue
				}

				if resp.StatusCode != 200 {
					log.Printf("got %d status code %d resp: %s", i, resp.StatusCode, string(b))
				}

				err = resp.Body.Close()
				if err != nil {
					log.Printf("got %d err: %s", i, err)

					continue
				}

				log.Printf("got %d status code %d", i, resp.StatusCode)
			}
		}(j)
	}

	wg.Wait()
}
