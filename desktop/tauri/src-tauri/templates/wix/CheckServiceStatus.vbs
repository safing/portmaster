Option Explicit

Dim objShell, objExec, strOutput, arrLines, i, arrStatus

' Create an instance of the WScript.Shell object
Set objShell = CreateObject("WScript.Shell")

' Run the sc.exe command to query the service
Set objExec = objShell.Exec("cmd /c sc.exe query PortmasterCore")

' Initialize an empty string to store the output
strOutput = ""

' Read all output from the command line
Do While Not objExec.StdOut.AtEndOfStream
    strOutput = strOutput & objExec.StdOut.ReadLine() & vbCrLf
Loop

' Split the output into lines
arrLines = Split(strOutput, vbCrLf)

' Example Output
' SERVICE_NAME: PortmasterCore
'         TYPE               : 10  WIN32_OWN_PROCESS
'         STATE              : 1  STOPPED
'         WIN32_EXIT_CODE    : 1077  (0x435)
'         SERVICE_EXIT_CODE  : 0  (0x0)
'         CHECKPOINT         : 0x0
'         WAIT_HINT          : 0x0

For i = LBound(arrLines) To UBound(arrLines)
	' Example line: STATE              : 1  STOPPED
    If InStr(arrLines(i), "STATE") > 0 Then
        ' Extract and display the service state
		' Example string: "1  STOPPED"
		arrStatus = Split(Trim(Mid(arrLines(i), InStr(arrLines(i), ":") + 1)), " ")
		' Anything other the STOPPED consider as running
		If Not arrStatus(2) = "STOPPED" Then
			 MsgBox("Portmaster service is running. Stop it and run the installer again.")
			 ' Notify the installer that it should fail.
			 WScript.Quit 1
		End If
    End If
Next