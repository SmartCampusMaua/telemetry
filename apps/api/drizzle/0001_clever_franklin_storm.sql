ALTER TABLE "device_buses" DROP CONSTRAINT "device_buses_device_id_devices_id_fk";
--> statement-breakpoint
ALTER TABLE "modbus_channels" DROP CONSTRAINT "modbus_channels_device_bus_id_device_buses_id_fk";
--> statement-breakpoint
ALTER TABLE "modbus_channels" DROP CONSTRAINT "modbus_channels_sensor_id_sensors_id_fk";
--> statement-breakpoint
ALTER TABLE "sensors" DROP CONSTRAINT "sensors_device_id_devices_id_fk";
--> statement-breakpoint
ALTER TABLE "sensors" ADD COLUMN "allow" boolean DEFAULT true NOT NULL;--> statement-breakpoint
ALTER TABLE "device_buses" ADD CONSTRAINT "device_buses_device_id_devices_id_fk" FOREIGN KEY ("device_id") REFERENCES "public"."devices"("id") ON DELETE cascade ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "modbus_channels" ADD CONSTRAINT "modbus_channels_device_bus_id_device_buses_id_fk" FOREIGN KEY ("device_bus_id") REFERENCES "public"."device_buses"("id") ON DELETE cascade ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "modbus_channels" ADD CONSTRAINT "modbus_channels_sensor_id_sensors_id_fk" FOREIGN KEY ("sensor_id") REFERENCES "public"."sensors"("id") ON DELETE cascade ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "sensors" ADD CONSTRAINT "sensors_device_id_devices_id_fk" FOREIGN KEY ("device_id") REFERENCES "public"."devices"("id") ON DELETE cascade ON UPDATE no action;