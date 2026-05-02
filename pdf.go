package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
)

func writeImagePDF(path string, images []string) error {
	var out bytes.Buffer
	write := func(format string, args ...any) { fmt.Fprintf(&out, format, args...) }
	offsets := []int{0}
	write("%%PDF-1.4\n")
	obj := 1
	catalogObj := obj
	obj++
	pagesObj := obj
	obj++
	type pageObj struct{ page, image, content int }
	objects := make([]pageObj, 0, len(images))
	for range images {
		objects = append(objects, pageObj{page: obj, image: obj + 1, content: obj + 2})
		obj += 3
	}
	offsets = make([]int, obj)
	offsets[catalogObj] = out.Len()
	write("%d 0 obj\n<< /Type /Catalog /Pages %d 0 R >>\nendobj\n", catalogObj, pagesObj)
	offsets[pagesObj] = out.Len()
	write("%d 0 obj\n<< /Type /Pages /Count %d /Kids [", pagesObj, len(objects))
	for _, po := range objects {
		write(" %d 0 R", po.page)
	}
	write(" ] >>\nendobj\n")
	for i, imgPath := range images {
		data, cfg, err := jpegData(imgPath)
		if err != nil {
			return err
		}
		content := fmt.Sprintf("q\n%.2f 0 0 %.2f 0 0 cm\n/Im%d Do\nQ\n", float64(pageWidth), float64(pageHeight), i+1)
		offsets[objects[i].page] = out.Len()
		write("%d 0 obj\n<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %d %d] /Resources << /XObject << /Im%d %d 0 R >> >> /Contents %d 0 R >>\nendobj\n",
			objects[i].page, pagesObj, pageWidth, pageHeight, i+1, objects[i].image, objects[i].content)
		offsets[objects[i].image] = out.Len()
		write("%d 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n",
			objects[i].image, cfg.Width, cfg.Height, len(data))
		out.Write(data)
		write("\nendstream\nendobj\n")
		offsets[objects[i].content] = out.Len()
		write("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", objects[i].content, len(content), content)
	}
	xref := out.Len()
	write("xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for i := 1; i < len(offsets); i++ {
		write("%010d 00000 n \n", offsets[i])
	}
	write("trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), catalogObj, xref)
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func jpegData(path string) ([]byte, image.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, image.Config{}, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, image.Config{}, err
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 92}); err != nil {
		return nil, image.Config{}, err
	}
	bounds := img.Bounds()
	cfg := image.Config{Width: bounds.Dx(), Height: bounds.Dy(), ColorModel: img.ColorModel()}
	return b.Bytes(), cfg, nil
}
