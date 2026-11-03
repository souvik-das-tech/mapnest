// WMS GetMap load test (PLAN.md §12, phase 11). Random 256 px tiles over India.
//
//   docker run --rm -i --network gis-net -e OWS=http://mapserver/ -e LAYERS=cities \
//     grafana/k6:0.57.0 run - < dev/loadtest/getmap.js
//
// Watch memory meanwhile: docker stats --no-stream mapserver msapi
import http from "k6/http";
import { check } from "k6";

const OWS = __ENV.OWS || "http://mapserver/";
const LAYERS = __ENV.LAYERS || "cities";

export const options = {
  scenarios: {
    ramp: {
      executor: "ramping-vus",
      stages: [
        { duration: "20s", target: 10 },
        { duration: "40s", target: 30 },
        { duration: "20s", target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<800"],
  },
};

// Web Mercator bounds of India, roughly.
const MINX = 7600000, MINY = 700000, MAXX = 10900000, MAXY = 4300000;

export default function () {
  const z = 2 ** (Math.floor(Math.random() * 4) + 2); // 4..32 tiles across
  const size = (MAXX - MINX) / z;
  const x = MINX + Math.floor(Math.random() * z) * size;
  const y = MINY + Math.floor(Math.random() * z) * size;
  const url =
    `${OWS}?SERVICE=WMS&VERSION=1.1.1&REQUEST=GetMap&LAYERS=${LAYERS}&STYLES=` +
    `&SRS=EPSG:3857&BBOX=${x},${y},${x + size},${y + size}&WIDTH=256&HEIGHT=256&FORMAT=image/png&TRANSPARENT=TRUE`;
  const res = http.get(url);
  check(res, { "is png": (r) => r.status === 200 && (r.headers["Content-Type"] || "").startsWith("image/png") });
}
