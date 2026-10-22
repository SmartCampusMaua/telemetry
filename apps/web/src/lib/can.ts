import { Dispatch, SetStateAction } from "react";

export type CanData = Array<{ timestamp: number, data: number }>

export type EletricMap = {
  PDMBatteryVoltage: CanData;
  PDMTotalCurrent: CanData;
  PDMInternalTemp: CanData;
  PDMGlobalErrorFlags: CanData;
  PDMInternalRailVoltage: CanData;
}

export let EletricMapInit = {
  PDMBatteryVoltage: [],
  PDMTotalCurrent: [],
  PDMInternalTemp: [],
  PDMGlobalErrorFlags: [],
  PDMInternalRailVoltage: []
}

export type MotorMap = {
  EngineCoolantTemperature: CanData;
  EngineOilTemperature: CanData;
  ManifoldAirTemperature: CanData;
  EngineOilPressure: CanData;
  EngineRPM: CanData;
  FuelLinePressure: CanData;
  FuelUsedRaw: CanData;
  ThrottlePosition: CanData;
  Lambda1: CanData;
  ManifoldAirPressure: CanData;
  Gear: CanData;
}

export let MotorMapInit = {
  EngineCoolantTemperature: [],
  EngineOilTemperature: [],
  ManifoldAirTemperature: [],
  EngineOilPressure: [],
  EngineRPM: [],
  FuelLinePressure: [],
  FuelUsedRaw: [],
  ThrottlePosition: [],
  Lambda1: [],
  ManifoldAirPressure: [],
  Gear: [],
}

export type DynamicsMap = {
  WheelSpeedFL: CanData;
  WheelSpeedFR: CanData;
  WheelSpeedRL: CanData;
  WheelSpeedRR: CanData;

  SteeringAngle: CanData;
  GForceLateral: CanData;
  GForceLongitudional: CanData;
  GForceVert: CanData;

  BrakePressureFront: CanData;
  BrakePressureRear: CanData;
}

export let DynamicsInit = {
  WheelSpeedFL: [],
  WheelSpeedFR: [],
  WheelSpeedRL: [],
  WheelSpeedRR: [],

  SteeringAngle: [],
  GForceLateral: [],
  GForceLongitudional: [],
  GForceVert: [],

  BrakePressureFront: [],
  BrakePressureRear: [],
}

export type GPSMap = {
      Latitude: CanData;
    Longitude: CanData;

    Altitude: CanData;
    GPSSpeed: CanData;
}


export let GPSInit = {
  Latitude: [],
  Longitude: [],

  Altitude: [],
  GPSSpeed: [],
}

const NATS_PACKET_SIZE = 34;

export function canParserMotor(message: Uint8Array, setCanList: Dispatch<SetStateAction<MotorMap>>) {
  for(let offset = 0; offset < message.length; offset++){
    const {timestamp, canId, canData} = getCanInfo(message, offset);
        switch (canId) {
            case 512:
                setCanList(
                    prev => {
                        prev.EngineRPM = [...prev.EngineRPM, {
                            timestamp: timestamp,
                            data: uint8ArrayToNumber(canData, 0, 2, true) * 0.1
                        }]
                        prev.ThrottlePosition = [...prev.ThrottlePosition, {
                            timestamp: timestamp,
                            data: uint8ArrayToNumber(canData, 2, 1, true) * 0.001
                        }]
                        prev.Lambda1 = [...prev.Lambda1, {
                            timestamp: timestamp,
                            data: uint8ArrayToNumber(canData, 3, 2, true)
                        }]
                        prev.ManifoldAirPressure = [...prev.ManifoldAirPressure, {
                            timestamp: timestamp,
                            data: uint8ArrayToNumber(canData, 5, 2, true)
                        }]
                        prev.Gear = [...prev.Gear, {
                            timestamp: timestamp,
                            data: uint8ArrayToNumber(canData, 7, 1, true)
                        }]
                        return { ...prev };
                    }
                )
                break;
            case 528:
                setCanList(
                    prev => {
                        prev.EngineCoolantTemperature = [...prev.EngineCoolantTemperature, {
                            timestamp: timestamp,
                          data: ((uint8ArrayToNumber(canData, 0, 2, true))) * 0.1
                        }]
                        prev.EngineOilTemperature = [...prev.EngineOilTemperature, {
                            timestamp: timestamp,
                          data: ((uint8ArrayToNumber(canData, 2, 2, true))) * 0.1
                        }]
                        prev.ManifoldAirTemperature = [...prev.ManifoldAirTemperature, {
                            timestamp: timestamp,
                          data: (uint8ArrayToNumber(canData, 4, 2, true )) * 0.1
                        }]
                        prev.EngineOilPressure = [...prev.EngineOilPressure, {
                            timestamp: timestamp,
                          data: uint8ArrayToNumber(canData, 6, 2, true)
                        }]
                        return { ...prev };
                    }
                )
                break;
            case 529:
                setCanList(
                    prev => {
                        prev.FuelLinePressure = [...prev.FuelLinePressure, {
                            timestamp: timestamp,
                            data: uint8ArrayToNumber(canData, 0, 2, true)
                        }]
                        prev.FuelUsedRaw = [...prev.FuelUsedRaw, {
                            timestamp: timestamp,
                            data: uint8ArrayToNumber(canData, 2, 4, true)
                        }]
                        return { ...prev };
                    }
                )
                break;
        }
    }
}

export function canParserEletric(message: Uint8Array, setCanList: Dispatch<SetStateAction<EletricMap>>) {
  for(let offset = 0; offset < message.length; offset++){
    const {timestamp, canId, canData} = getCanInfo(message, offset);
        switch (canId) {
          case 1024:
            setCanList(
              prev => {
                prev.PDMBatteryVoltage = [...prev.PDMBatteryVoltage, {
                  timestamp: timestamp,
                  data: (uint8ArrayToNumber(canData, 0, 2, true)) * 0.01 
                }]
                prev.PDMTotalCurrent = [...prev.PDMInternalTemp, {
                  timestamp: Number(timestamp),
                  data: (uint8ArrayToNumber(canData, 2, 2, true)) * 0.1
                }] 
                prev.PDMInternalTemp = [...prev.PDMInternalTemp, {
                  timestamp: Number(timestamp),
                  data: (uint8ArrayToNumber(canData, 4, 2, true))
                }]
                return { ...prev };
              }
            )
            break;
        }
    }
}

export function canParserDynamics(message: Uint8Array, setCanList: Dispatch<SetStateAction<DynamicsMap>>) {
  for(let offset = 0; offset < message.length; offset++){
    const {timestamp, canId, canData} = getCanInfo(message, offset);
        switch (canId) {
                    case 256:
                setCanList(
                    prev => {
                        prev.WheelSpeedFL = [...prev.WheelSpeedFL, {
                            timestamp: timestamp,
                          data: uint8ArrayToNumber(canData, 0, 2, true) * 0.1
                        }]
                        prev.WheelSpeedFR = [...prev.WheelSpeedFR, {
                            timestamp: timestamp,
                          data: uint8ArrayToNumber(canData, 2, 2, true) * 0.1
                        }]
                        prev.WheelSpeedRL = [...prev.WheelSpeedRL, {
                            timestamp: timestamp,
                          data: uint8ArrayToNumber(canData, 4, 2, true) * 0.1
                        }]
                        prev.WheelSpeedRR = [...prev.WheelSpeedRR, {
                            timestamp: timestamp,
                          data: uint8ArrayToNumber(canData, 6, 2, true) * 0.1
                        }]
                        return { ...prev };
                    }
                )
                break;
            case 272:
                setCanList(
                    prev => {
                        prev.SteeringAngle = [...prev.SteeringAngle, {
                            timestamp: timestamp,
                          data: (uint8ArrayToNumber(canData, 0, 2, true)) * 0.1
                        }]
                        prev.GForceLateral = [...prev.GForceLateral, {
                            timestamp: timestamp,
                          data: (uint8ArrayToNumber(canData, 2, 2, true)) * 0.01
                        }]
                        prev.GForceLongitudional = [...prev.GForceLongitudional, {
                            timestamp: timestamp,
                          data: (uint8ArrayToNumber(canData, 4, 2, true)) * 0.01
                        }]
                        prev.GForceVert = [...prev.GForceVert, {
                            timestamp: timestamp,
                          data: (uint8ArrayToNumber(canData, 6, 2, true )) * 0.01
                        }]
                        return { ...prev };
                    }
                )
                break;
          case 288:
                setCanList(
                  prev => {
                        prev.BrakePressureFront = [...prev.BrakePressureFront, {
                            timestamp: timestamp,
                          data: uint8ArrayToNumber(canData, 0, 2, true)
                        }]
                        prev.BrakePressureRear = [...prev.BrakePressureRear, {
                            timestamp: timestamp,
                          data: uint8ArrayToNumber(canData, 2, 2, true)
                        }]
                        return { ...prev };
                    }
                )
                break;
        }
    }
}

export function canParserGPS(message: Uint8Array, setCanList: Dispatch<SetStateAction<GPSMap>>) {
  for(let offset = 0; offset < message.length; offset++){
    const {timestamp, canId, canData} = getCanInfo(message, offset);
    switch (canId) {
      case 768:
        setCanList(
          prev => {
            prev.Latitude = [...prev.Latitude, {
              timestamp: timestamp,
              data: (uint8ArrayToNumber(canData, 0, 4, true)) 
            }]
            prev.Longitude = [...prev.Longitude, {
              timestamp: Number(timestamp),
              data: (uint8ArrayToNumber(canData, 4, 8, true))
            }]
            return { ...prev };
          }
        )
        break;
      case 784:
        setCanList(
          prev => {
            prev.Altitude = [...prev.Altitude, {
              timestamp: timestamp,
              data: (uint8ArrayToNumber(canData, 0, 2, true)) 
            }]
            prev.GPSSpeed = [...prev.GPSSpeed, {
              timestamp: Number(timestamp),
              data: (uint8ArrayToNumber(canData, 2, 2, true))
            }]
            return { ...prev };
          }
        )
    }
  }
}


function uint8ArrayToNumber(byteArray: Uint8Array, offset: number, size: number, bigEndian: boolean): number {
    let result = 0;
    for (let i = 0; i < size; i++) {
        result = result | byteArray[offset + i];
        if (i == size - 1) break;
        result = result << 8;
    }
  result = result << 24 >> 24;
    return result;
}

function getCanInfo(message : Uint8Array, offset: number){
  const timestamp = Date.now();
  const canIdArray = message.subarray(19 + NATS_PACKET_SIZE * offset, 23 + NATS_PACKET_SIZE * offset).toReversed();
  const canId = canIdArray[2] << 8 | canIdArray[3];
  const canData = message.subarray(24 + NATS_PACKET_SIZE * offset, 32 + NATS_PACKET_SIZE * offset);
  return {timestamp, canId, canData};
}
