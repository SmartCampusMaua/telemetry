"use client"

import { useEffect, useState } from "react"
import { LineCanChart } from "../lineChart";
import { getNatsConnection } from "@/lib/nats";
import { canParserEletric, canParserMotor, EletricMap, EletricMapInit, MotorMap, MotorMapInit } from "@/lib/can";
import { NatsConnection } from "@nats-io/nats-core";



export default function EletricPage(){
  const [canMap, setCanMap] = useState<EletricMap>(EletricMapInit);
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
            canParserEletric(msg.data, setCanMap)
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
        <LineCanChart data={canMap.PDMBatteryVoltage} title="PDM Battery Voltage" unit="V"/>
        <LineCanChart data={canMap.PDMTotalCurrent} title="PDM Total Current"/>
      </div>
      <div className="flex">
        <LineCanChart data={canMap.PDMInternalTemp} title="PDM Internal Temp"/>
      </div>
    </div>
  );
}
