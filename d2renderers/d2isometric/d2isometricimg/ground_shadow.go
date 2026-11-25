package d2isometricimg

import "math"

// Project the whole caster for its diffuse shadow. Shorter, sharper casts
// from geometry close to the receiver anchor the object at its footprint.
// Heights and filter widths are in scene units, independent of output size.
var groundShadowLayers = [...]struct{ height, sigma, opacity float64 }{
	{math.Inf(1), .085, .079},
	{.65, .062, .117},
	{.20, .034, .170},
}

type rasterGroundShadow struct {
	camera rasterCamera // same light space for every layer and animation frame
	alpha  []uint16
	size   int
	scale  float64
}

type groundShadowMask struct {
	alpha         []uint8
	size          int
	scale, weight float64
}

// A triangular filter of integer radius r followed by linear interpolation
// has a standard deviation of (r+1)/sqrt(6) texels. Choose a grid per layer
// to keep its world-space softness and cap filtering at nine taps per axis.
func groundShadowGrid(light rasterCamera, sigma float64) (scale float64, radius int) {
	radius = max(1, min(4, int(math.Floor(sigma*light.scale*math.Sqrt(6)))-1))
	return min(light.scale, float64(radius+1)/(math.Sqrt(6)*sigma)), radius
}

func buildGroundShadow(w *rasterWork, ts []Triangle, ground float64, light rasterCamera) (rasterGroundShadow, error) {
	shadow := rasterGroundShadow{camera: light}
	receiver := ground + rasterShadowNormalOffset
	minY := receiver + max(.012, 2/light.scale)*light.direction.Y
	var layers []groundShadowMask
	var span float64
	for _, layer := range groundShadowLayers {
		scale, radius := groundShadowGrid(light, layer.sigma)
		camera := light
		camera.scale = scale
		// Leave a clear border around the light frustum for the filter and
		// for bilinear reads beyond the silhouette at the edge of the atlas.
		size := int(math.Ceil(float64(light.width)*scale/light.scale)) + 2*(2*radius+2)
		camera.width, camera.height = size, size
		span = max(span, float64(size)/scale)
		shadow.scale = max(shadow.scale, scale)
		atlas, err := rasterizeShadow(w, ts, camera, minY, receiver+layer.height)
		if err != nil {
			return shadow, err
		}
		if len(atlas.opacity) == 0 {
			continue
		}
		if err := blurGroundMask(w, atlas.opacity, size, radius); err != nil {
			return shadow, err
		}
		layers = append(layers, groundShadowMask{alpha: atlas.opacity, size: size, scale: scale, weight: layer.opacity / 255})
	}
	if len(layers) == 0 {
		return shadow, nil
	}
	// Compose once in light space. Stills and animation strips then need a
	// single filtered read per ground sample, even on maximum-sized canvases.
	// Retain the entire filter border, even when the atlas size is capped.
	shadow.size = min(light.width, int(math.Ceil(span*shadow.scale)))
	shadow.scale = float64(shadow.size) / span
	shadow.alpha = make([]uint16, shadow.size*shadow.size)
	if err := w.charge(int64(len(shadow.alpha)) * int64(len(layers)) * 4); err != nil {
		return shadow, err
	}
	for y := 0; y < shadow.size; y++ {
		if y%32 == 0 {
			if err := w.ctx.Err(); err != nil {
				return shadow, err
			}
		}
		v := (float64(y) - float64(shadow.size-1)/2) / shadow.scale
		for x := 0; x < shadow.size; x++ {
			u := (float64(x) - float64(shadow.size-1)/2) / shadow.scale
			remaining := 1.
			for _, m := range layers {
				remaining *= 1 - m.weight*groundBilinear(m.alpha, m.size, u*m.scale, v*m.scale)
			}
			shadow.alpha[y*shadow.size+x] = uint16(math.Round((1 - remaining) * 65535))
		}
	}
	return shadow, nil
}

// Apply the same compact separable kernel to the assembled silhouette, so
// adjacent triangles cannot create seams or accumulate excess opacity.
func blurGroundMask(w *rasterWork, alpha []uint8, size, radius int) error {
	if err := w.charge(int64(size) * int64(size) * int64(2*(2*radius+1))); err != nil {
		return err
	}
	scratch := make([]uint8, len(alpha))
	divisor := (radius + 1) * (radius + 1)
	for _, step := range []int{1, size} {
		for y := radius; y < size-radius; y++ {
			if y%32 == 0 {
				if err := w.ctx.Err(); err != nil {
					return err
				}
			}
			for x := radius; x < size-radius; x++ {
				at := y*size + x
				sum := (radius + 1) * int(alpha[at])
				for k := 1; k <= radius; k++ {
					sum += (radius + 1 - k) * (int(alpha[at-k*step]) + int(alpha[at+k*step]))
				}
				scratch[at] = uint8((sum + divisor/2) / divisor)
			}
		}
		alpha, scratch = scratch, alpha
	}
	return nil
}

func (s *rasterGroundShadow) coverage(p Vec) float64 {
	p.Y += rasterShadowNormalOffset
	x := (rasterDot(p, s.camera.right) - s.camera.centerX) * s.scale
	y := (s.camera.centerY - rasterDot(p, s.camera.up)) * s.scale
	return groundBilinear(s.alpha, s.size, x, y) / 65535
}

func groundBilinear[T uint8 | uint16](alpha []T, size int, x, y float64) float64 {
	u, v := x+float64(size-1)/2, y+float64(size-1)/2
	if u < 0 || v < 0 || u >= float64(size-1) || v >= float64(size-1) {
		return 0
	}
	i, j := int(u), int(v)
	fx, fy := u-float64(i), v-float64(j)
	at := j*size + i
	top := (1-fx)*float64(alpha[at]) + fx*float64(alpha[at+1])
	bottom := (1-fx)*float64(alpha[at+size]) + fx*float64(alpha[at+size+1])
	return (1-fy)*top + fy*bottom
}
