"use client"

import { useEffect, useState } from "react"
import { LineCanChart } from "../lineChart";
import { getNatsConnection } from "@/lib/nats";
import { canParserMotor, MotorMap, MotorMapInit } from "@/lib/can";
import { NatsConnection } from "@nats-io/nats-core";



export default function MotorPage(){
  const [canMap, setCanMap] = useState<MotorMap>(MotorMapInit);
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
            canParserMotor(msg.data, setCanMap);
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
        <LineCanChart data={canMap.EngineCoolantTemperature} title="Engine Coolant Temperature" unit="ºC"/>
        <LineCanChart data={canMap.EngineOilPressure} title="Engine Oil Pressure" unit="kPa"/>
      </div>
      <div className="flex">
        <LineCanChart data={canMap.EngineOilTemperature} title="Engine Oil Temperature" unit="kPa"/>
        <LineCanChart data={canMap.EngineRPM} title="Engine RPM" unit="RPM"/>
      </div>
      <div className="flex">
        <LineCanChart data={canMap.Gear} title="Gear"/>
        <LineCanChart data={canMap.Lambda1} title="Lambda 1"/>
      </div>
      <div>
        <LineCanChart data={canMap.FuelLinePressure} title="Fuel Line Pressure" />
        <LineCanChart data={canMap.FuelUsedRaw} title="Fuel Used Raw"/>
      </div>
    </div>
  );
}
