"use client"

import { useEffect, useState } from "react"
import { LineCanChart } from "../lineChart";
import { getNatsConnection } from "@/lib/nats";
import { canParserDynamics, canParserEletric, canParserMotor, DynamicsInit, DynamicsMap, EletricMap, EletricMapInit, MotorMap, MotorMapInit } from "@/lib/can";
import { NatsConnection } from "@nats-io/nats-core";



export default function DynamicsPage(){
  const [canMap, setCanMap] = useState<DynamicsMap>(DynamicsInit);
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
            canParserDynamics(msg.data, setCanMap)
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
        <LineCanChart title="Wheel SpeedFL" data={canMap.WheelSpeedFL} unit="km/h"/>
        <LineCanChart title="Wheel SpeedFR" data={canMap.WheelSpeedFR} unit="km/h"/>
        <LineCanChart title="Wheel SpeedRL" data={canMap.WheelSpeedRL} unit="km/h"/>
        <LineCanChart title="Wheel SpeedRR" data={canMap.WheelSpeedRR} unit="km/h"/>
      </div>
      <div className="flex">
        <LineCanChart title="Steering Angle" data={canMap.SteeringAngle} unit="°"/>
        <LineCanChart title="GForceLateral" data={canMap.GForceLateral} unit="G"/>
        <LineCanChart title="GForceLongitudional" data={canMap.GForceLongitudional} unit="G"/>
        <LineCanChart title="GForceVert" data={canMap.GForceVert} unit="G"/>
      </div>
    </div>
  );
}
