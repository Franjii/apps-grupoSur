package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
)

func main() {
	// Load source logo
	f, err := os.Open("static/images/simple-logo.png")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open logo: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	logo, err := png.Decode(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to decode logo: %v\n", err)
		os.Exit(1)
	}

	// Brand primary color for background
	brandColor := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

	sizes := []int{72, 96, 128, 144, 152, 180, 192, 384, 512}

	for _, size := range sizes {
		img := createIcon(logo, brandColor, size, 0.75)
		path := fmt.Sprintf("static/images/icons/icon-%dx%d.png", size, size)
		if err := saveImage(img, path); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("Generated %s\n", path)
	}

	// Maskable icon with extra padding (safe zone = 80%, so logo at 60% for safety)
	maskable := createIcon(logo, brandColor, 512, 0.60)
	path := "static/images/icons/icon-512x512-maskable.png"
	if err := saveImage(maskable, path); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s\n", path)
}

// createIcon generates a square icon with the logo centered on a colored background.
// logoScale controls how much of the icon the logo occupies (0.0 to 1.0).
func createIcon(logo image.Image, bg color.Color, size int, logoScale float64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	// Fill background
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	// Calculate logo dimensions preserving aspect ratio
	logoBounds := logo.Bounds()
	logoW := logoBounds.Dx()
	logoH := logoBounds.Dy()

	maxDim := int(float64(size) * logoScale)

	var newW, newH int
	if float64(logoW)/float64(logoH) > 1 {
		newW = maxDim
		newH = int(float64(logoH) * float64(maxDim) / float64(logoW))
	} else {
		newH = maxDim
		newW = int(float64(logoW) * float64(maxDim) / float64(logoH))
	}

	// Simple nearest-neighbor scaling
	scaled := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		srcY := y * logoH / newH
		for x := 0; x < newW; x++ {
			srcX := x * logoW / newW
			scaled.Set(x, y, logo.At(logoBounds.Min.X+srcX, logoBounds.Min.Y+srcY))
		}
	}

	// Center the scaled logo
	offsetX := (size - newW) / 2
	offsetY := (size - newH) / 2
	draw.Draw(img, image.Rect(offsetX, offsetY, offsetX+newW, offsetY+newH), scaled, image.Point{}, draw.Over)

	return img
}

func saveImage(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
