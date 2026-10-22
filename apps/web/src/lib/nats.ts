import { wsconnect, NatsConnection } from "@nats-io/nats-core";


let natsClient: NatsConnection | null = null;

export async function getNatsConnection(): Promise<NatsConnection> {
  if (!natsClient || natsClient.isClosed()) {
    try{
    natsClient = await wsconnect({
      servers: process.env.NEXT_PUBLIC_NATS_URL || "ws://localhost:4223",
    });
    } catch(e){
      console.log(e);
    }
  }
  return natsClient!;
}
