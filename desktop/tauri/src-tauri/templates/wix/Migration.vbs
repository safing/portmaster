Dim FSO
Set FSO = CreateObject("Scripting.FileSystemObject")

Dim customData, args, oldInstallationDir, migrationFlagFile, newDataDir, doMigration
customData = Session.Property("CustomActionData")

' Split the string by commas
args = Split(customData, ",")

' Access individual arguments
Dim commonAppDataFolder, programMenuFolder, startupFolder, appDataFolder
commonAppDataFolder = Trim(args(0))
programMenuFolder = Trim(args(1))
startupFolder = Trim(args(2))
appDataFolder = Trim(args(3))

' Read variables from the session object
oldInstallationDir = commonAppDataFolder & "Safing\Portmaster\"
newDataDir = commonAppDataFolder & "Portmaster"
migrationFlagFile = oldInstallationDir & "migrated.txt"
doMigration = true

' Check for existing installtion
If Not fso.FolderExists(oldInstallationDir) Then
	doMigration = false
End If

' Check if migration was already done
If fso.FileExists(migrationFlagFile) Then
	doMigration = false
End If

If doMigration Then
	' Copy the config file
	dim configFile
	configFile = "config.json"
	If fso.FileExists(oldInstallationDir & configFile) Then
		fso.CopyFile oldInstallationDir & configFile, newDataDir & configFile
	End If

	' Copy the database folder
	dim databaseFolder
	databaseFolder = "databases"
	If fso.FolderExists(oldInstallationDir & databaseFolder) Then
		fso.CopyFolder oldInstallationDir & databaseFolder, newDataDir & databaseFolder
	End If

	' Delete shortcuts
	dim shortcutsFolder
	notifierShortcut = programMenuFolder & "Portmaster/Portmaster Notifier.lnk"
	If fso.FileExists(notifierShortcut) Then
		fso.DeleteFile notifierShortcut, True
	End If

	' Delete startup shortcut
	dim srartupFile
	srartupFile = startupFolder & "Portmaster Notifier.lnk"
	If fso.FileExists(srartupFile) Then
		fso.DeleteFile srartupFile, True
	End If

	' Delete shortuct in user folder
	dim userShortcut
	userShortcut = appDataFolder & "Microsoft\Windows\Start Menu\Programs\Portmaster.lnk"
	If fso.FileExists(userShortcut) Then
		fso.DeleteFile userShortcut, True
	End If

	' Delete the old installer
	dim oldUninstaller
	oldUninstaller = oldInstallationDir & "portmaster-uninstaller.exe"
	If fso.FileExists(oldUninstaller) Then
		fso.DeleteFile oldUninstaller, True
	End If

	' Set the migration flag file
	fso.CreateTextFile(migrationFlagFile).Close
End If