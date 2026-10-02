
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
#include <stdarg.h>
#include <stdbool.h>
#include <ntstrsafe.h>

#include "abi_layout.h"

NTSTATUS pm_InitDriverObject(
    DRIVER_OBJECT *driverObject,
    UNICODE_STRING *registryPath,
    WDFDRIVER *driver,
    WDFDEVICE *device,
    wchar_t *win_device_name,
    wchar_t *dos_device_name,
    PFN_WDF_DRIVER_UNLOAD wdfEventUnload,
    PFN_WDFDEVICE_WDM_IRP_PREPROCESS createCallback,
    PFN_WDFDEVICE_WDM_IRP_PREPROCESS cleanupCallback,
    PFN_WDFDEVICE_WDM_IRP_PREPROCESS closeCallback,
    PFN_WDFDEVICE_WDM_IRP_PREPROCESS readCallback,
    PFN_WDFDEVICE_WDM_IRP_PREPROCESS writeCallback,
    PFN_WDFDEVICE_WDM_IRP_PREPROCESS deviceControlCallback)
{
    UNICODE_STRING deviceName = { 0 };
    RtlInitUnicodeString(&deviceName, win_device_name);

    // Keep output handles in a known state on every failure path. WdfDeviceCreate
    // replaces deviceInit with NULL on success and stores the created device in
    // *device, which must be explicitly deleted if link creation fails below.
    *driver = NULL;
    *device = NULL;

    UNICODE_STRING deviceSymlink = { 0 };
    RtlInitUnicodeString(&deviceSymlink, dos_device_name);

    // Create a WDFDRIVER for this driver.
    WDF_DRIVER_CONFIG config = { 0 };
    WDF_DRIVER_CONFIG_INIT(&config, WDF_NO_EVENT_CALLBACK);
    config.DriverInitFlags = WdfDriverInitNonPnpDriver;
    config.EvtDriverUnload = wdfEventUnload;
    NTSTATUS status = WdfDriverCreate(
        driverObject,
        registryPath,
        WDF_NO_OBJECT_ATTRIBUTES,
        &config,
        driver);
    if (!NT_SUCCESS(status)) {
        return status;
    }

    // Create a control WDFDEVICE for this non-PnP driver.
    PWDFDEVICE_INIT deviceInit = WdfControlDeviceInitAllocate(
        *driver,
        &SDDL_DEVOBJ_SYS_ALL_ADM_ALL); // only admins and kernel can access device
    if (!deviceInit) {
        return STATUS_INSUFFICIENT_RESOURCES;
    }

    // SystemBuffer is used by the Rust preprocess callbacks for read/write and
    // METHOD_BUFFERED IOCTL requests. Buffered I/O is also the KMDF default, but
    // set it explicitly so that the native and Rust contracts cannot drift.
    WdfDeviceInitSetIoType(deviceInit, WdfDeviceIoBuffered);
    WdfDeviceInitSetDeviceType(deviceInit, FILE_DEVICE_NETWORK);
    WdfDeviceInitSetCharacteristics(deviceInit, FILE_DEVICE_SECURE_OPEN, FALSE);

    status = WdfDeviceInitAssignName(deviceInit, &deviceName);
    if (!NT_SUCCESS(status)) {
        goto Exit;
    }
    // This WDFDEVICE_INIT describes a non-PnP control device, not a child PDO.
    // Access is fixed by the explicit SDDL above, so it does not need setup-class
    // registry overrides either.

#define PM_REGISTER_PREPROCESS(callback, majorFunction) \
    status = WdfDeviceInitAssignWdmIrpPreprocessCallback( \
        deviceInit, callback, majorFunction, NULL, 0); \
    if (!NT_SUCCESS(status)) { \
        goto Exit; \
    }

    PM_REGISTER_PREPROCESS(createCallback, IRP_MJ_CREATE);
    PM_REGISTER_PREPROCESS(cleanupCallback, IRP_MJ_CLEANUP);
    PM_REGISTER_PREPROCESS(closeCallback, IRP_MJ_CLOSE);
    PM_REGISTER_PREPROCESS(readCallback, IRP_MJ_READ);
    PM_REGISTER_PREPROCESS(writeCallback, IRP_MJ_WRITE);
    PM_REGISTER_PREPROCESS(deviceControlCallback, IRP_MJ_DEVICE_CONTROL);

#undef PM_REGISTER_PREPROCESS

    status = WdfDeviceCreate(&deviceInit, WDF_NO_OBJECT_ATTRIBUTES, device);
    if (!NT_SUCCESS(status)) {
        goto Exit;
    }

    status = WdfDeviceCreateSymbolicLink(*device, &deviceSymlink);

Exit:
    if (deviceInit) {
        WdfDeviceInitFree(deviceInit);
    }
    if (!NT_SUCCESS(status) && *device) {
        // WdfDeviceCreate has consumed deviceInit by this point. Delete the
        // partially initialized control device when a later setup step fails;
        // otherwise the WDFDEVICE and its symbolic-link state remain owned by a
        // DriverEntry path that is about to return failure.
        WdfObjectDelete(*device);
        *device = NULL;
    }
    return status;
}

void pm_FinishControlDeviceInitialization(WDFDEVICE device)
{
    // Until this call, KMDF does not deliver I/O or WMI requests to the control
    // device. Rust invokes it only after all WFP state and the global Device have
    // been fully initialized and published.
    WdfControlFinishInitializing(device);
}

DEVICE_OBJECT* pm_GetDeviceObject(WDFDEVICE device) {
    return WdfDeviceWdmGetDeviceObject(device);
}
