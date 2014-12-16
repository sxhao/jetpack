package jetpack

import "encoding/json"
import "fmt"
import "io/ioutil"
import "os"
import "path"
import "path/filepath"
import "strings"
import "time"

import "code.google.com/p/go-uuid/uuid"
import "github.com/appc/spec/schema"
import "github.com/appc/spec/schema/types"
import "github.com/juju/errors"

type ImageMetadata struct {
	Hash      types.Hash
	Origin    string
	Timestamp time.Time
}

type Image struct {
	ImageMetadata
	schema.ImageManifest
	UUID uuid.UUID
	DS   Dataset `json:"-"`
}

func (img *Image) PPPrepare() interface{} {
	return struct {
		Manifest         schema.ImageManifest
		Meta             ImageMetadata
		UUID             uuid.UUID
		ZFSDataset, Path string
	}{
		img.ImageManifest,
		img.ImageMetadata,
		img.UUID,
		img.DS.Name,
		img.DS.Mountpoint,
	}
}

func newImage(ds *Dataset) (*Image, error) {
	img := &Image{
		DS:   *ds,
		UUID: uuid.Parse(path.Base(ds.Name)),
	}
	if img.UUID == nil {
		return nil, errors.New("Invalid UUID")
	}
	return img, nil
}

func GetImage(ds *Dataset) (*Image, error) {
	img, err := newImage(ds)
	if err != nil {
		return nil, errors.Trace(err)
	}

	metadataJSON, err := ioutil.ReadFile(img.Path("metadata"))
	if err != nil {
		// FIXME: if metadata does not exist (yet), this means that image
		// is being created (imported/built), this should not be treated
		// as error, maybe as input to separate cleanup task.
		return nil, errors.Trace(err)
	}

	if err = json.Unmarshal(metadataJSON, &img.ImageMetadata); err != nil {
		return nil, errors.Trace(err)
	}

	if err := img.readManifest(); err != nil {
		return nil, errors.Trace(err)
	}

	return img, nil
}

func ImportImage(ds *Dataset, uri string) (*Image, error) {
	img, err := newImage(ds)
	if err != nil {
		return nil, errors.Trace(err)
	}

	img.Origin = uri
	img.Timestamp = time.Now()

	if hash, err := UnpackImage(uri, img.Path()); err != nil {
		// TODO: cleanup
		return nil, errors.Trace(err)
	} else {
		img.Hash = hash
	}

	if err := img.readManifest(); err != nil {
		// TODO: cleanup
		return nil, errors.Trace(err)
	}

	// Serialize metadata
	if metadataJSON, err := json.Marshal(img.ImageMetadata); err != nil {
		// TODO: cleanup
		return nil, errors.Trace(err)
	} else {
		if err := ioutil.WriteFile(img.Path("metadata"), metadataJSON, 0400); err != nil {
			// TODO: cleanup
			return nil, errors.Trace(err)
		}
	}

	if _, err := img.DS.Snapshot("aci", false); err != nil {
		// TODO: cleanup
		return nil, errors.Trace(err)
	}

	if err := img.DS.SetProperty("readonly", "on"); err != nil {
		// TODO: cleanup
		return nil, errors.Trace(err)
	}

	return img, nil
}

func (img *Image) Path(pieces ...string) string {
	return filepath.Join(append([]string{img.DS.Mountpoint}, pieces...)...)
}

func (img *Image) readManifest() error {
	manifestJSON, err := ioutil.ReadFile(img.Path("manifest"))
	if err != nil {
		return errors.Trace(err)
	}

	if err = json.Unmarshal(manifestJSON, &img.ImageManifest); err != nil {
		return errors.Trace(err)
	}

	return nil
}

func (img *Image) String() string {
	return fmt.Sprintf("ACI:%v(%v)", img.Name, img.Hash)
}

func (img *Image) PrettyLabels() string {
	labels := make([]string, len(img.Labels))
	for i, l := range img.Labels {
		labels[i] = fmt.Sprintf("%v=%#v", l.Name, l.Value)
	}
	return strings.Join(labels, " ")
}

func (img *Image) Clone(dest string) (*Dataset, error) {
	snap, err := img.DS.Snapshot("aci", false)
	if err != nil {
		return nil, errors.Trace(err)
	}

	ds, err := snap.Clone(dest, nil)
	if err != nil {
		return nil, errors.Trace(err)
	}

	// FIXME: maybe not? (hint: multi-app containers)
	for _, filename := range []string{"manifest", "metadata"} {
		if err := os.Remove(ds.Path(filename)); err != nil && !os.IsNotExist(err) {
			return nil, errors.Trace(err)
		}
	}
	return ds, nil
}

// For sorting
type ImageSlice []*Image

func (ii ImageSlice) Len() int           { return len(ii) }
func (ii ImageSlice) Less(i, j int) bool { return ii[i].Name < ii[j].Name }
func (ii ImageSlice) Swap(i, j int)      { ii[i], ii[j] = ii[j], ii[i] }