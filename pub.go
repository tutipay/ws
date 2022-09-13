package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/go-redis/redis/v9"
)

//CashoutPub subscribes to messages occurs in chan_cashouts. We should push onto this channel via a publisher
func CashoutPub(r *redis.Client) {
	ctx := context.Background()
	pubsub := r.Subscribe(ctx, "chan_cashouts")

	// Wait for confirmation that subscription is created before publishing anything.
	_, err := pubsub.Receive(ctx)
	if err != nil {
		log.Printf("There is an error in connecting to chan.")
		return
	}

	// // Go channel which receives messages.
	ch := pubsub.Channel()

	// Consume messages.
	var message Message
	for msg := range ch {
		// So this is how we gonna do it! So great!
		// we have to parse the payload here:
		if err := json.Unmarshal([]byte(msg.Payload), &message); err != nil {
			log.Printf("Error in marshaling data: %v", err)
			continue
		}

		data, err := json.Marshal(&message)
		if err != nil {
			log.Printf("Error in marshaling response: %v", err)
			continue
		}
		log.Printf("the data is: %v", data)
		// _, err = http.Post(message.Endpoint, "application/json", bytes.NewBuffer(data))
		// Maybe send a push notification message here in case we need to tell the user they have a message

		fmt.Println(msg.Channel, msg.Payload)
	}
}
