"use client"

import { useEffect, useState } from "react"
import { LineCanChart } from "../lineChart";
import { getNatsConnection } from "@/lib/nats";
import { canParserDynamics, canParserEletric, canParserGPS, canParserMotor, DynamicsInit, DynamicsMap, EletricMap, EletricMapInit, GPSInit, GPSMap, MotorMap, MotorMapInit } from "@/lib/can";
import { NatsConnection } from "@nats-io/nats-core";



export default function GPSPage(){
  const [canMap, setCanMap] = useState<GPSMap>(GPSInit);
  useEffect(
    () => {
      let nc : NatsConnection | null;
      async function getData(){
        nc = await getNatsConnection();
        if(nc == null){
          return;
        }
        nc.subscribe('zc2x.can.rsu', {
          callback: (err, msg) => {
            canParserGPS(msg.data, setCanMap)
          }
        });
      }
      getData();
      return () =>{
        nc?.close();
      }
    },[]);
  return (
    <div className="w-[100%]">
      <div className="flex">
        <LineCanChart title="GPS Speed" unit="km/h" data={canMap.GPSSpeed}/>
        <LineCanChart title="Altitude" unit="m" data={canMap.Altitude}/>
      </div>
      <div className="flex">
        <LineCanChart title="Latitude"  unit="°" data={canMap.Latitude}/>
        <LineCanChart title="Longitude" unit="°" data={canMap.Longitude}/>
      </div>
    </div>
  );
}
