// dllmain.cpp : Defines the entry point for the DLL application.
#include "pch.h"

#pragma comment(lib, "tdh.lib")

// GUID of the DNS log provider
static const GUID DNS_CLIENT_PROVIDER_GUID = {
    0x1C95126E,
    0x7EEA,
    0x49A9,
    {0xA3, 0xFE, 0xA3, 0x78, 0xB0, 0x3D, 0xDB, 0x4D} };

// GUID of the event session. This should be unique for the application.
static const GUID PORTMASTER_ETW_SESSION_GUID = {
    0x0211d070,
    0xc3b2,
    0x4609,
    {0x92, 0xf5, 0x28, 0xe7, 0x18, 0xb2, 0x3b, 0x18} };

// Name of the session. This is visble when user queries all ETW sessions.
// (example `logman query -ets`)
#define LOGSESSION_NAME L"PortmasterDNSEventListener"

// Fuction type of the callback that will be called on each event.
typedef uint64_t(*GoEventRecordCallback)(wchar_t* domain, uint32_t pid, wchar_t* result);

// Holds the state of the ETW Session.
struct ETWSessionState {
    TRACEHANDLE SessionTraceHandle;
    EVENT_TRACE_PROPERTIES* SessionProperties;
    TRACEHANDLE sessionHandle;
    GoEventRecordCallback callback;
};

// getPropertyValue reads a property from the event.
static bool getPropertyValue(PEVENT_RECORD evt, LPWSTR prop, PBYTE* pData) {
    // Describe the data that needs to be retrieved from the event.
    PROPERTY_DATA_DESCRIPTOR DataDescriptor;
    ZeroMemory(&DataDescriptor, sizeof(DataDescriptor));
    DataDescriptor.PropertyName = (ULONGLONG)(prop);
    DataDescriptor.ArrayIndex = 0;

    DWORD PropertySize = 0;
    // Check if the data is available and what is the size of it.
    DWORD status =
        TdhGetPropertySize(evt, 0, NULL, 1, &DataDescriptor, &PropertySize);
    if (ERROR_SUCCESS != status) {
        return false;
    }

    // Allocate memory for the data.
    *pData = (PBYTE)malloc(PropertySize);
    if (NULL == *pData) {
        return false;
    }

    // Get the data.
    status =
        TdhGetProperty(evt, 0, NULL, 1, &DataDescriptor, PropertySize, *pData);
    if (ERROR_SUCCESS != status) {
        if (*pData) {
            free(*pData);
            *pData = NULL;
        }
        return false;
    }

    return true;
}

// EventRecordCallback is a callback called on each event.
static void WINAPI EventRecordCallback(PEVENT_RECORD eventRecord) {
    PBYTE resultValue = NULL;
    PBYTE domainValue = NULL;

    getPropertyValue(eventRecord, (LPWSTR)L"QueryResults", &resultValue);
    getPropertyValue(eventRecord, (LPWSTR)L"QueryName", &domainValue);

    ETWSessionState* state = (ETWSessionState*)eventRecord->UserContext;

    if (resultValue != NULL && domainValue != NULL) {
        state->callback((wchar_t*)domainValue, eventRecord->EventHeader.ProcessId, (wchar_t*)resultValue);
    }

    free(resultValue);
    free(domainValue);
}

extern "C" {
    // PM_ETWCreateState allocates memory for the state and initializes the config for the session. PM_ETWDestroySession must be called to avoid leaks.
    // callback must be set to a valid function pointer.
    __declspec(dllexport) ETWSessionState* PM_ETWCreateState(GoEventRecordCallback callback) {
        // Create trace session properties.
        ULONG BufferSize = sizeof(EVENT_TRACE_PROPERTIES) + sizeof(LOGSESSION_NAME);
        EVENT_TRACE_PROPERTIES* SessionProperties =
            (EVENT_TRACE_PROPERTIES*)calloc(1, BufferSize);
        SessionProperties->Wnode.BufferSize = BufferSize;
        SessionProperties->Wnode.Flags = WNODE_FLAG_TRACED_GUID;
        SessionProperties->Wnode.ClientContext = 1; // QPC clock resolution
        SessionProperties->Wnode.Guid = PORTMASTER_ETW_SESSION_GUID;
        SessionProperties->LogFileMode = EVENT_TRACE_REAL_TIME_MODE;
        SessionProperties->MaximumFileSize = 1; // MB
        SessionProperties->LoggerNameOffset = sizeof(EVENT_TRACE_PROPERTIES);

        // Create state
        ETWSessionState* state = (ETWSessionState*)calloc(1, sizeof(ETWSessionState));
        state->SessionProperties = SessionProperties;
        state->callback = callback;
        return state;
    }

    // PM_ETWInitializeSession initializes the session.
    __declspec(dllexport) uint32_t PM_ETWInitializeSession(ETWSessionState* state) {
        return StartTrace(&state->SessionTraceHandle, LOGSESSION_NAME,
            state->SessionProperties);
    }

    // PM_ETWStartTrace subscribes to the dns events and start listening. The function blocks while the listener is running.
    // Call PM_ETWStopTrace to stop the listener.
    __declspec(dllexport) uint32_t PM_ETWStartTrace(ETWSessionState* state) {
        ULONG status =
            EnableTraceEx2(state->SessionTraceHandle, (LPCGUID)&DNS_CLIENT_PROVIDER_GUID,
                EVENT_CONTROL_CODE_ENABLE_PROVIDER,
                TRACE_LEVEL_INFORMATION, 0, 0, 0, NULL);

        if (status != ERROR_SUCCESS) {
            return status;
        }

        EVENT_TRACE_LOGFILE trace = { 0 };

        trace.LoggerName = (LPWSTR)(LOGSESSION_NAME);
        trace.ProcessTraceMode =
            PROCESS_TRACE_MODE_REAL_TIME | PROCESS_TRACE_MODE_EVENT_RECORD;
        trace.EventRecordCallback = EventRecordCallback;
        trace.Context = state;

        state->sessionHandle = OpenTrace(&trace);
        if (state->sessionHandle == INVALID_PROCESSTRACE_HANDLE) {
            return 1;
        }

        status = ProcessTrace(&state->sessionHandle, 1, NULL, NULL);
        if (status != ERROR_SUCCESS) {
            return 1;
        }

        return ERROR_SUCCESS;
    }

    // PM_ETWFlushTrace flushes the event buffer.
    __declspec(dllexport) uint32_t PM_ETWFlushTrace(ETWSessionState* state) {
        return ControlTrace(state->SessionTraceHandle, LOGSESSION_NAME,
            state->SessionProperties, EVENT_TRACE_CONTROL_FLUSH);
    }

    // PM_ETWFlushTrace stops the listener.
    __declspec(dllexport) uint32_t PM_ETWStopTrace(ETWSessionState* state) {
        return ControlTrace(state->SessionTraceHandle, LOGSESSION_NAME, state->SessionProperties,
            EVENT_TRACE_CONTROL_STOP);
    }

    // PM_ETWFlushTrace Closes the session and frees recourses.
    __declspec(dllexport) uint32_t PM_ETWDestroySession(ETWSessionState* state) {
        if (state == NULL) {
            return 1;
        }
        uint32_t status = CloseTrace(state->sessionHandle);

        // Free memory.
        free(state->SessionProperties);
        free(state);
        return status;
    }

    // PM_ETWStopOldSession removes old session with the same name if they exist. 
    // It returns success(0) only if its able to delete the old session.
    __declspec(dllexport) ULONG PM_ETWStopOldSession() {
        ULONG status = ERROR_SUCCESS;
        TRACEHANDLE sessionHandle = 0;

        // Create trace session properties
        size_t bufferSize = sizeof(EVENT_TRACE_PROPERTIES) + sizeof(LOGSESSION_NAME);
        EVENT_TRACE_PROPERTIES* sessionProperties = (EVENT_TRACE_PROPERTIES*)calloc(1, bufferSize);
        sessionProperties->Wnode.BufferSize = (ULONG)bufferSize;
        sessionProperties->Wnode.Flags = WNODE_FLAG_TRACED_GUID;
        sessionProperties->Wnode.ClientContext = 1; // QPC clock resolution
        sessionProperties->Wnode.Guid = PORTMASTER_ETW_SESSION_GUID;
        sessionProperties->LoggerNameOffset = sizeof(EVENT_TRACE_PROPERTIES);

        // Use Control trace will stop the session which will trigger a delete.
        status = ControlTrace(NULL, LOGSESSION_NAME, sessionProperties, EVENT_TRACE_CONTROL_STOP);

        free(sessionProperties);
        return status;
    }
}