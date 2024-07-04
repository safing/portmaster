
/*
 *  Name:        helper.c
 */

#include <stdlib.h>
#include <wchar.h>

#define NDIS640 1                // Windows 8 and Windows Server 2012

#include "Ntifs.h"
#include <ntddk.h>              // Windows Driver Development Kit
#include <wdf.h>                // Windows Driver Foundation

#pragma warning(push)
#pragma warning(disable: 4201)  // Disable "Nameless struct/union" compiler warning for fwpsk.h only!
#include <fwpsk.h>              // Functions and enumerated types used to implement callouts in kernel mode
#pragma warning(pop)            // Re-enable "Nameless struct/union" compiler warning

#include <fwpmk.h>              // Functions used for managing IKE and AuthIP main mode (MM) policy and security associations
#include <fwpvi.h>              // Mappings of OS specific function versions (i.e. fn's that end in 0 or 1)
#include <guiddef.h>            // Used to define GUID's
#include <initguid.h>           // Used to define GUID's
#include "devguid.h"
#include <stdarg.h>
#include <stdbool.h>
#include <ntstrsafe.h>

EVT_WDF_DRIVER_UNLOAD emptyEventUnload;

NTSTATUS pm_InitDriverObject(DRIVER_OBJECT * driverObject, UNICODE_STRING * registryPath, WDFDRIVER * driver, WDFDEVICE * device, wchar_t *win_device_name, wchar_t *dos_device_name, WDF_OBJECT_ATTRIBUTES * objectAttributes, void (*wdfEventUnload)(WDFDRIVER)) {
	UNICODE_STRING deviceName = { 0 };
	RtlInitUnicodeString(&deviceName, win_device_name);

	UNICODE_STRING deviceSymlink = { 0 };
	RtlInitUnicodeString(&deviceSymlink, dos_device_name);

	// Create a WDFDRIVER for this driver
	WDF_DRIVER_CONFIG config = { 0 };
	WDF_DRIVER_CONFIG_INIT(&config, WDF_NO_EVENT_CALLBACK);
	config.DriverInitFlags = WdfDriverInitNonPnpDriver;
	config.EvtDriverUnload = wdfEventUnload; // <-- Necessary for this driver to unload correctly
	NTSTATUS status = WdfDriverCreate(driverObject, registryPath, WDF_NO_OBJECT_ATTRIBUTES, &config, driver);
	if (!NT_SUCCESS(status)) {
      return status;
	}

	// Create a WDFDEVICE for this driver
	PWDFDEVICE_INIT deviceInit = WdfControlDeviceInitAllocate(*driver, &SDDL_DEVOBJ_SYS_ALL_ADM_ALL);  // only admins and kernel can access device
	if (!deviceInit) {
	    return STATUS_INSUFFICIENT_RESOURCES;
	}

	// Configure the WDFDEVICE_INIT with a name to allow for access from user mode
	WdfDeviceInitSetDeviceType(deviceInit, FILE_DEVICE_NETWORK);
	WdfDeviceInitSetCharacteristics(deviceInit, FILE_DEVICE_SECURE_OPEN, false);
	(void) WdfDeviceInitAssignName(deviceInit, &deviceName);
	(void) WdfPdoInitAssignRawDevice(deviceInit, &GUID_DEVCLASS_NET);
	WdfDeviceInitSetDeviceClass(deviceInit, &GUID_DEVCLASS_NET);

	status = WdfDeviceCreate(&deviceInit, objectAttributes, device);
	if (!NT_SUCCESS(status)) {
	  WdfDeviceInitFree(deviceInit);
		return status;
	}
	status = WdfDeviceCreateSymbolicLink(*device, &deviceSymlink);
	if (!NT_SUCCESS(status)) {
		return status;
	}

	// The system will not send I/O requests or Windows Management Instrumentation (WMI) requests to a control device object unless the driver has called WdfControlFinishInitializing.
	WdfControlFinishInitializing(*device);

	return STATUS_SUCCESS;
}

void* pm_WdfObjectGetTypedContextWorker(WDFOBJECT wdfObject, PCWDF_OBJECT_CONTEXT_TYPE_INFO typeInfo) {
    return WdfObjectGetTypedContextWorker(wdfObject, typeInfo->UniqueType);
}

DEVICE_OBJECT* pm_GetDeviceObject(WDFDEVICE device) {
    return WdfDeviceWdmGetDeviceObject(device);
}

UINT64 pm_QuerySystemTime() {
	UINT64 timestamp = 0;
	KeQuerySystemTime(&timestamp);
	return timestamp;
}