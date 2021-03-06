/*
Copyright 2019 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kubectl

import (
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/build"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/docker"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/warnings"
)

// GetImages gathers a map of base image names to the image with its tag
func (l *ManifestList) GetImages() ([]build.Artifact, error) {
	s := &imageSaver{}
	_, err := l.Visit(s)
	return s.Images, err
}

type imageSaver struct {
	ReplaceAny
	Images []build.Artifact
}

func (is *imageSaver) Matches(key interface{}) bool {
	return key == "image"
}

func (is *imageSaver) NewValue(old interface{}) (bool, interface{}) {
	image, ok := old.(string)
	if !ok {
		return false, nil
	}
	parsed, err := docker.ParseReference(image)
	if err != nil {
		return false, err
	}

	is.Images = append(is.Images, build.Artifact{
		Tag:       image,
		ImageName: parsed.BaseName,
	})
	return false, nil
}

// ReplaceImages replaces image names in a list of manifests.
func (l *ManifestList) ReplaceImages(builds []build.Artifact, defaultRepo string) (ManifestList, error) {
	replacer := newImageReplacer(builds, defaultRepo)

	updated, err := l.Visit(replacer)
	if err != nil {
		return nil, errors.Wrap(err, "replacing images")
	}

	replacer.Check()
	logrus.Debugln("manifests with tagged images:", updated.String())

	return updated, nil
}

type imageReplacer struct {
	ReplaceAny
	defaultRepo     string
	tagsByImageName map[string]string
	found           map[string]bool
}

func newImageReplacer(builds []build.Artifact, defaultRepo string) *imageReplacer {
	tagsByImageName := make(map[string]string)
	for _, build := range builds {
		tagsByImageName[build.ImageName] = build.Tag
	}

	return &imageReplacer{
		defaultRepo:     defaultRepo,
		tagsByImageName: tagsByImageName,
		found:           make(map[string]bool),
	}
}

func (r *imageReplacer) Matches(key interface{}) bool {
	return key == "image"
}

func (r *imageReplacer) NewValue(old interface{}) (bool, interface{}) {
	image, ok := old.(string)
	if !ok {
		return false, nil
	}

	found, tag := r.parseAndReplace(image)
	if !found {
		subbedImage := r.substituteRepoIntoImage(image)
		if image == subbedImage {
			return found, tag
		}
		// no match, so try substituting in defaultRepo value
		found, tag = r.parseAndReplace(subbedImage)
	}
	return found, tag
}

func (r *imageReplacer) parseAndReplace(image string) (bool, interface{}) {
	parsed, err := docker.ParseReference(image)
	if err != nil {
		warnings.Printf("Couldn't parse image [%s]: %s", image, err.Error())
		return false, nil
	}

	// Leave images referenced by digest as they are
	if parsed.Digest != "" {
		return false, nil
	}

	if tag, present := r.tagsByImageName[parsed.BaseName]; present {
		r.found[parsed.BaseName] = true
		return true, tag
	}

	return false, nil
}

func (r *imageReplacer) Check() {
	for imageName := range r.tagsByImageName {
		if !r.found[imageName] {
			warnings.Printf("image [%s] is not used by the deployment", imageName)
		}
	}
}

func (r *imageReplacer) substituteRepoIntoImage(originalImage string) string {
	return util.SubstituteDefaultRepoIntoImage(r.defaultRepo, originalImage)
}
