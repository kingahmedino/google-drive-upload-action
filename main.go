// TTW Software Team
// Mathis Van Eetvelde
// 2021-present

// Modified by Aditya Karnam
// 2021
// Added file overwrite support

// Modified by Ahmed Mohammed
// 2023
// Added support for returning uploaded file url

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sethvargo/go-githubactions"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

const (
	scope                    = "https://www.googleapis.com/auth/drive.file"
	filenameInput            = "filename"
	nameInput                = "name"
	folderIdInput            = "folderId"
	credentialsInput         = "credentials"
	overwrite                = "false"
	mimeTypeInput            = "mimeType"
	useCompleteSourceName    = "useCompleteSourceFilenameAsName"
	mirrorDirectoryStructure = "mirrorDirectoryStructure"
	namePrefixInput          = "namePrefix"
)

func uploadToDrive(svc *drive.Service, filename string, folderId string, driveFile *drive.File, name string, mimeType string) (string, error) {
	fi, err := os.Lstat(filename)
	if err != nil {
		return "", fmt.Errorf("lstat of file with filename: %v failed with error: %v", filename, err)
	}
	if fi.IsDir() {
		fmt.Printf("%s is a directory. skipping upload.", filename)
		return "", nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("opening file with filename: %v failed with error: %v", filename, err)
	}

	var updatedFile *drive.File
	if driveFile != nil {
		f := &drive.File{
			Name:     name,
			MimeType: mimeType,
		}
		updatedFile, err = svc.Files.Update(driveFile.Id, f).AddParents(folderId).Media(file).SupportsAllDrives(true).Do()
	} else {
		f := &drive.File{
			Name:     name,
			MimeType: mimeType,
			Parents:  []string{folderId},
		}
		updatedFile, err = svc.Files.Create(f).Media(file).SupportsAllDrives(true).Do()
	}

	if err != nil {
		return "", fmt.Errorf("creating/updating file failed with error: %v", err)
	}

	link := fmt.Sprintf("https://drive.google.com/file/d/%s/view", updatedFile.Id)
	return link, nil
}

func main() {
	// get filename argument from action input
	filename := githubactions.GetInput(filenameInput)
	if filename == "" {
		missingInput(filenameInput)
	}
	files, err := filepath.Glob(filename)
	fmt.Printf("Files: %v\n", files)
	if err != nil {
		githubactions.Fatalf(fmt.Sprintf("Invalid filename pattern: %v", err))
	}
	if len(files) == 0 {
		githubactions.Fatalf(fmt.Sprintf("No file found! pattern: %s", filename))
	}

	// get overwrite flag
	var overwriteFlag bool
	overwrite := githubactions.GetInput("overwrite")
	if overwrite == "" {
		githubactions.Warningf("Overwrite is disabled.")
		overwriteFlag = false
	} else {
		overwriteFlag, _ = strconv.ParseBool(overwrite)
	}
	// get name argument from action input
	name := githubactions.GetInput(nameInput)

	// get folderId argument from action input
	folderId := githubactions.GetInput(folderIdInput)
	if folderId == "" {
		missingInput(folderIdInput)
	}

	// get file mimeType argument from action input
	mimeType := githubactions.GetInput(mimeTypeInput)

	// get optional flags
	useCompleteSourceNameFlag, _ := strconv.ParseBool(githubactions.GetInput(useCompleteSourceName))
	mirrorDirectoryStructureFlag, _ := strconv.ParseBool(githubactions.GetInput(mirrorDirectoryStructure))
	namePrefix := githubactions.GetInput(namePrefixInput)

	// get credentials from action input
	credentials := githubactions.GetInput(credentialsInput)
	if credentials == "" {
		missingInput(credentialsInput)
	}

	// decode credentials from base64
	decodedCreds, err := base64.StdEncoding.DecodeString(credentials)
	if err != nil {
		githubactions.Fatalf(fmt.Sprintf("Failed to decode credentials: %v", err))
	}

	// create a JWT config from the credentials
	jwtConfig, err := google.JWTConfigFromJSON(decodedCreds, scope)
	if err != nil {
		githubactions.Fatalf(fmt.Sprintf("Failed to create JWT config: %v", err))
	}

	// create a context and client for Google Drive API
	ctx := context.Background()
	client := jwtConfig.Client(ctx)

	// create a new drive service client
	svc, err := drive.NewService(ctx, drive.WithHTTPClient(client))
	if err != nil {
		githubactions.Fatalf(fmt.Sprintf("Failed to create Drive service client: %v", err))
	}

	// iterate over files matching the pattern
	for _, file := range files {
		// handle file names with spaces
		escapedName := strings.Replace(file, " ", "\\ ", -1)

		// create directory structure if mirrorDirectoryStructure flag is enabled
		if mirrorDirectoryStructureFlag {
			fileDir := filepath.Dir(escapedName)
			if fileDir != "." {
				_, err = createDriveDirectory(svc, folderId, fileDir)
				if err != nil {
					githubactions.Fatalf(fmt.Sprintf("Failed to create directory structure on Google Drive: %v", err))
				}
			}
		}

		// determine the name for the uploaded file
		var uploadedFileName string
		if useCompleteSourceNameFlag {
			uploadedFileName = file
		} else {
			baseFileName := filepath.Base(file)
			if namePrefix != "" {
				uploadedFileName = namePrefix + baseFileName
			} else if name != "" {
				uploadedFileName = name
			} else {
				uploadedFileName = baseFileName
			}
		}

		// check if the file already exists in the folder
		driveFile, err := findFileByName(svc, uploadedFileName, folderId)
		if err != nil {
			githubactions.Fatalf(fmt.Sprintf("Failed to check existing files in the folder: %v", err))
		}

		// upload the file to Google Drive
		uploadedLink, err := uploadToDrive(svc, file, folderId, driveFile, uploadedFileName, mimeType)
		if err != nil {
			githubactions.Fatalf(fmt.Sprintf("Failed to upload file to Google Drive: %v", err))
		}

		// print the link to the uploaded file
		githubactions.Infof("Uploaded file: %s", uploadedLink)
	}
}

func createDriveDirectory(svc *drive.Service, parentFolderID, folderName string) (string, error) {
	folder, err := findFileByName(svc, folderName, parentFolderID)
	if err != nil {
		return "", fmt.Errorf("failed to check existing folders in the parent folder: %v", err)
	}
	if folder != nil {
		return folder.Id, nil
	}

	d := &drive.File{
		Name:     folderName,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentFolderID},
	}

	newFolder, err := svc.Files.Create(d).SupportsAllDrives(true).Do()
	if err != nil {
		return "", fmt.Errorf("failed to create directory on Google Drive: %v", err)
	}

	return newFolder.Id, nil
}

func findFileByName(svc *drive.Service, name, parentFolderID string) (*drive.File, error) {
	query := fmt.Sprintf("name = '%s' and '%s' in parents and trashed = false", name, parentFolderID)
	files, err := svc.Files.List().Q(query).Do()
	if err != nil {
		return nil, fmt.Errorf("error searching for file by name: %v", err)
	}
	if len(files.Files) > 0 {
		return files.Files[0], nil
	}
	return nil, nil
}

func missingInput(inputName string) {
	githubactions.Fatalf(fmt.Sprintf("Input %s is missing or empty", inputName))
}
