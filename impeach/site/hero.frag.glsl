#version 300 es
precision highp float;

// The light table.
//
// A sheet of heavy uncoated stock under an examination lamp. The testimony is
// printed on top in the DOM. This shader draws the paper, the lamp, and the
// record that soaked into the fibers underneath, which only shows where the
// light falls. Where the record contradicts the testimony the ink bleeds red.

uniform vec2  uResolution;
uniform float uTime;
uniform vec2  uLight;        // lamp centre, canvas pixels
uniform float uLightRadius;  // pixels
uniform float uStamp;        // 0 to 1, the verdict landing
uniform vec2  uStampOrigin;  // canvas pixels
uniform vec4  uBleed[4];     // xy = min, zw = max, in UV space
uniform int   uBleedCount;
uniform float uDark;         // 0 light scheme, 1 dark
uniform sampler2D uRecordTex;
// 0 hands the record back to the DOM. Under reduced motion the lamp is fixed,
// and a fixed cone clips lines wider than itself, so the shader must not be
// the only place the evidence exists.
uniform float uRecordOn;

out vec4 outColor;

// ---------------------------------------------------------------- noise

float hash(vec2 p) {
  p = fract(p * vec2(123.34, 456.21));
  p += dot(p, p + 45.32);
  return fract(p.x * p.y);
}

float valueNoise(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  // Smoothstep the interpolant so the grain has no grid artefacts.
  vec2 u = f * f * (3.0 - 2.0 * f);
  float a = hash(i);
  float b = hash(i + vec2(1.0, 0.0));
  float c = hash(i + vec2(0.0, 1.0));
  float d = hash(i + vec2(1.0, 1.0));
  return mix(mix(a, b, u.x), mix(c, d, u.x), u.y);
}

// Two octaves is enough for paper. More reads as sand.
float grain(vec2 p) {
  return valueNoise(p) * 0.65 + valueNoise(p * 2.7) * 0.35;
}

// A low frequency direction field, so the stock has a fiber lay rather than
// isotropic mush.
float fibers(vec2 uv) {
  float a = valueNoise(uv * vec2(3.0, 22.0));
  float b = valueNoise(uv * vec2(26.0, 2.4) + 11.0);
  return mix(a, b, 0.45);
}

// ------------------------------------------------------------------ main

void main() {
  vec2 fragPx = gl_FragCoord.xy;
  vec2 uv = fragPx / uResolution;
  // Flip once here: the record texture is drawn in Canvas2D, top left origin.
  vec2 texUV = vec2(uv.x, 1.0 - uv.y);

  // ---- paper -------------------------------------------------------------
  vec3 paperLight = vec3(1.0);
  vec3 paperDark  = vec3(0.090, 0.098, 0.110);
  vec3 paper = mix(paperLight, paperDark, uDark);

  float g = grain(fragPx * 0.9);
  float f = fibers(uv);

  // Grain is subtractive on white stock and additive on dark, so both read as
  // texture rather than as dirt.
  float grainAmt = mix(0.030, 0.045, uDark);
  paper += (mix(-1.0, 1.0, uDark)) * (g - 0.5) * grainAmt;
  paper += (mix(-1.0, 1.0, uDark)) * (f - 0.5) * grainAmt * 0.5;

  // A faint vignette. The sheet is lit from above, not evenly.
  float vig = 1.0 - 0.16 * pow(length((uv - 0.5) * vec2(1.05, 1.0)) * 1.35, 2.2);
  paper *= mix(vig, mix(vig, 1.0, 0.4), uDark);

  // ---- the stamp ripple --------------------------------------------------
  // A damped radial deformation of the sampling coordinates, so the paper
  // visibly flexes when the verdict lands.
  vec2 rippleUV = uv;
  if (uStamp > 0.0 && uStamp < 1.0) {
    vec2 d = uv - (uStampOrigin / uResolution);  // already in GL space
    float r = length(d);
    float wave = sin(r * 46.0 - uStamp * 13.0) * exp(-r * 7.0) * (1.0 - uStamp);
    rippleUV += normalize(d + 1e-5) * wave * 0.006;
    paper += wave * 0.05 * (1.0 - uDark * 0.5);
  }

  // ---- the lamp ----------------------------------------------------------
  // Slightly elliptical, soft edged. Shorter dimension sets the radius so it
  // is the same physical size in portrait and landscape.
  vec2 toLight = (fragPx - uLight) / vec2(uLightRadius * 1.18, uLightRadius);
  float d = length(toLight);
  float lamp = 1.0 - smoothstep(0.28, 1.05, d);
  lamp = pow(lamp, 1.15);

  vec3 lampColor = vec3(1.0, 0.957, 0.863); // --lamp #FFF4DC
  // The lamp warms and lifts the paper, and lifts grain contrast inside the
  // cone so the surface looks raked by light.
  paper = mix(paper, paper * lampColor + lampColor * 0.16, lamp * mix(0.72, 0.9, uDark));
  paper += (g - 0.5) * 0.07 * lamp;

  // ---- the record --------------------------------------------------------
  // Domain warp, so the ink reads as absorbed into fibers rather than laid on
  // top as a flat overlay.
  vec2 warp = vec2(
    valueNoise(rippleUV * 210.0) - 0.5,
    valueNoise(rippleUV * 210.0 + 37.0) - 0.5
  ) * (1.5 / uResolution.x) * 2.2;

  float ink   = texture(uRecordTex, texUV + warp).a;
  // A second, offset sample at low alpha gives a wet edge.
  float wet   = texture(uRecordTex, texUV + warp + vec2(0.8 / uResolution.x, 0.0)).a;
  float inkA  = clamp(ink + wet * 0.20, 0.0, 1.0);

  vec3 recordLight = vec3(0.169, 0.290, 0.478); // --record-ink #2B4A7A
  vec3 recordDark  = vec3(0.561, 0.690, 0.878); // #8FB0E0
  vec3 recordCol   = mix(recordLight, recordDark, uDark);

  // ---- the bleed ---------------------------------------------------------
  // Inside a bleed rectangle the ink tints toward red and rises to full
  // strength, and it seeps a little past the edges through the same warp.
  float bleedMask = 0.0;
  vec2 warpedUV = texUV + warp * 1.6;
  for (int i = 0; i < 4; i++) {
    if (i >= uBleedCount) break;
    vec4 r = uBleed[i];
    // Feather the rectangle so the edge is absorbed, not cut.
    vec2 inside = smoothstep(r.xy - 0.006, r.xy + 0.004, warpedUV)
                * (1.0 - smoothstep(r.zw - 0.004, r.zw + 0.006, warpedUV));
    bleedMask = max(bleedMask, inside.x * inside.y);
  }

  vec3 bleedLight = vec3(0.549, 0.114, 0.094); // --bleed #8C1D18
  vec3 bleedDark  = vec3(0.871, 0.361, 0.341); // --bleed dark #DE5C57
  vec3 bleedCol   = mix(bleedLight, bleedDark, uDark);

  // A slow pulse, so the contradiction looks wet rather than printed.
  float pulse = 0.92 + 0.08 * sin(uTime * (6.2831853 / 4.0));

  vec3  inkCol   = mix(recordCol, bleedCol, bleedMask);
  float inkAlpha = inkA * mix(1.0, pulse, bleedMask);
  // Under the lamp only. This is the whole idea: the record is invisible in
  // ordinary light. The bleed reaches full strength where the light lands.
  inkAlpha *= mix(lamp * 0.92, min(1.0, lamp * 1.35), bleedMask);

  vec3 col = mix(paper, inkCol, clamp(inkAlpha * uRecordOn, 0.0, 1.0));

  outColor = vec4(col, 1.0);
}
