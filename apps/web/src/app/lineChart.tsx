"use client"
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import { CanData } from "@/lib/can"

const chartConfig = {
  desktop: {
    label: "data",
    color: "var(--chart-1)",
  },
} satisfies ChartConfig

export function LineCanChart({title, data, unit} : {title : string, data:CanData, unit?: string}) {
  return (
    <Card className="w-[50%]">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{unit}</CardDescription>
      </CardHeader>
      <CardContent>
        <ChartContainer config={chartConfig} className="w-[100%] h-[100%]">
          <LineChart
            accessibilityLayer
            data={data.sort((a,b)=>{return a.timestamp - b.timestamp}).slice(-100)}
            margin={{
              left: 12,
              right: 12,
            }}
          >
            <CartesianGrid />
            <XAxis
              dataKey="timestamp"
              tickLine={false}
              axisLine={false}
              tickMargin={8}
              
            />
            <YAxis/>
            <ChartTooltip
              cursor={true}
              content={<ChartTooltipContent hideLabel />}
            />
            <Line
              dataKey="data"
              type="linear"
              stroke="var(--color-desktop)"
              strokeWidth={2}
              dot={false}
              
            />
          </LineChart>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}
