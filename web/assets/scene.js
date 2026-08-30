import * as THREE from "/vendor/three.module.min.js";

const canvas = document.getElementById("scene");
const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

if (canvas && !reduced) {
  try {
    start(canvas);
  } catch {
    canvas.remove();
  }
}

function start(canvas) {
  const renderer = new THREE.WebGLRenderer({ canvas, alpha: true, antialias: true });
  renderer.setPixelRatio(Math.min(devicePixelRatio, 2));

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(40, 1, 0.1, 60);
  camera.position.set(0, 0, 8);

  const world = new THREE.Group();
  scene.add(world);

  const small = innerWidth < 760;
  const size = 2.4;
  const count = small ? 1400 : 3000;

  const cubeTargets = new Float32Array(count * 3);
  const starts = new Float32Array(count * 3);
  const delays = new Float32Array(count);

  for (let i = 0; i < count; i++) {
    const face = Math.floor(Math.random() * 6);
    const axis = face % 3;
    const sign = face < 3 ? 1 : -1;
    const a = (Math.random() - 0.5) * size;
    const b = (Math.random() - 0.5) * size;
    const point = [0, 0, 0];
    point[axis] = (sign * size) / 2;
    point[(axis + 1) % 3] = a;
    point[(axis + 2) % 3] = b;
    cubeTargets[i * 3] = point[0];
    cubeTargets[i * 3 + 1] = point[1];
    cubeTargets[i * 3 + 2] = point[2];

    const direction = new THREE.Vector3().randomDirection();
    const radius = 4.5 + Math.random() * 3.5;
    starts[i * 3] = direction.x * radius;
    starts[i * 3 + 1] = direction.y * radius;
    starts[i * 3 + 2] = direction.z * radius;

    delays[i] = Math.random() * 1.1;
  }

  const exploded = new Float32Array(count * 3);
  for (let i = 0; i < count; i++) {
    const i3 = i * 3;
    const scale = 2.1 + Math.random() * 0.5;
    exploded[i3] = cubeTargets[i3] * scale;
    exploded[i3 + 1] = cubeTargets[i3 + 1] * scale;
    exploded[i3 + 2] = cubeTargets[i3 + 2] * scale + (Math.random() - 0.5) * 1.4;
  }

  const clusters = new Float32Array(count * 3);
  const centers = [];
  const columns = [-3.1, 0, 3.1];
  for (const y of [1.7, -1.7]) {
    for (const x of columns) centers.push([x, y]);
  }
  for (let i = 0; i < count; i++) {
    const center = centers[i % centers.length];
    const angle = Math.random() * Math.PI * 2;
    const radius = Math.sqrt(Math.random()) * 0.55;
    clusters[i * 3] = center[0] + Math.cos(angle) * radius;
    clusters[i * 3 + 1] = center[1] + Math.sin(angle) * radius;
    clusters[i * 3 + 2] = (Math.random() - 0.5) * 0.8;
  }

  const ribbon = new Float32Array(count * 3);
  for (let i = 0; i < count; i++) {
    const t = i / count;
    ribbon[i * 3] = -6 + t * 12 + (Math.random() - 0.5) * 0.25;
    ribbon[i * 3 + 1] = Math.sin(t * Math.PI * 5) * 0.3 + (Math.random() - 0.5) * 0.3;
    ribbon[i * 3 + 2] = (Math.random() - 0.5) * 0.8;
  }

  const current = new Float32Array(count * 3);
  const weights = { cube: 1, exploded: 0, clusters: 0, ribbon: 0 };

  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute("position", new THREE.BufferAttribute(current, 3));
  const material = new THREE.PointsMaterial({
    color: 0xffd84a,
    size: small ? 0.05 : 0.034,
    transparent: true,
    opacity: 0.9,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
    sizeAttenuation: true,
  });
  const points = new THREE.Points(geometry, material);
  world.add(points);

  const edges = new THREE.LineSegments(
    new THREE.EdgesGeometry(new THREE.BoxGeometry(size, size, size)),
    new THREE.LineBasicMaterial({ color: 0xffd84a, transparent: true, opacity: 0.3 }),
  );
  world.add(edges);

  const ringCount = small ? 200 : 400;
  const ringPositions = new Float32Array(ringCount * 3);
  for (let i = 0; i < ringCount; i++) {
    const angle = (i / ringCount) * Math.PI * 2;
    ringPositions[i * 3] = Math.cos(angle) * 3.1;
    ringPositions[i * 3 + 1] = (Math.random() - 0.5) * 0.05;
    ringPositions[i * 3 + 2] = Math.sin(angle) * 3.1;
  }
  const ringGeometry = new THREE.BufferGeometry();
  ringGeometry.setAttribute("position", new THREE.BufferAttribute(ringPositions, 3));
  const ring = new THREE.Points(
    ringGeometry,
    new THREE.PointsMaterial({
      color: 0xeab308,
      size: 0.026,
      transparent: true,
      opacity: 0.5,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    }),
  );
  ring.rotation.set(1.05, 0, 0.45);
  world.add(ring);

  const drift = new Float32Array(count);
  for (let i = 0; i < count; i++) drift[i] = Math.random();

  const clock = new THREE.Clock();
  let visible = true;
  let pointerX = 0;
  let pointerY = 0;
  let progress = 0;
  let targetProgress = 0;

  const state = {
    cube: 1, exploded: 0, clusters: 0, ribbon: 0,
    camZ: 8, x: 2.6, y: 0, dim: 1, spin: 1,
  };
  const target = { ...state };

  const chapters = [];
  document.querySelectorAll("[data-chapter]").forEach((element) => {
    chapters.push({
      element,
      at: 0,
      state: readState(element.dataset.chapter),
    });
  });

  function readState(raw) {
    const next = { cube: 0, exploded: 0, clusters: 0, ribbon: 0, camZ: 8, x: 0, y: 0, dim: 1, spin: 1 };
    for (const pair of raw.split(";")) {
      const [key, value] = pair.split(":");
      if (key in next) next[key] = Number(value);
    }
    return next;
  }

  const measure = () => {
    const denominator = Math.max(document.documentElement.scrollHeight - innerHeight, 1);
    for (const chapter of chapters) {
      chapter.at = Math.min(Math.max((chapter.element.offsetTop - innerHeight * 0.5) / denominator, 0), 1);
    }
    chapters.sort((a, b) => a.at - b.at);
  };
  measure();
  addEventListener("resize", () => {
    resize();
    measure();
  });

  const smooth = (t) => t * t * (3 - 2 * t);

  const applyChapter = () => {
    if (!chapters.length) return;
    let index = 0;
    while (index + 1 < chapters.length && progress >= chapters[index + 1].at) index += 1;
    const from = chapters[index];
    const to = chapters[Math.min(index + 1, chapters.length - 1)];
    const span = Math.max(to.at - from.at, 0.0001);
    const t = smooth(Math.min(Math.max((progress - from.at) / span, 0), 1));
    for (const key in state) {
      target[key] = from.state[key] + (to.state[key] - from.state[key]) * t;
    }
  };

  addEventListener("scroll", () => {
    const denominator = Math.max(document.documentElement.scrollHeight - innerHeight, 1);
    targetProgress = Math.min(Math.max(scrollY / denominator, 0), 1);
  }, { passive: true });

  addEventListener("pointermove", (event) => {
    pointerX = (event.clientX / innerWidth) * 2 - 1;
    pointerY = (event.clientY / innerHeight) * 2 - 1;
  }, { passive: true });

  const resize = () => {
    renderer.setSize(innerWidth, innerHeight, false);
    camera.aspect = innerWidth / innerHeight;
    camera.updateProjectionMatrix();
  };
  resize();

  new IntersectionObserver((entries) => {
    visible = entries[0].isIntersecting;
  }, { threshold: 0 }).observe(canvas);

  const lerp = (from, to, amount) => from + (to - from) * amount;

  renderer.setAnimationLoop(() => {
    if (!visible || document.hidden) return;
    const time = clock.getElapsedTime();

    progress = lerp(progress, targetProgress, 0.06);
    applyChapter();

    const ease = 0.045;
    for (const key in state) state[key] = lerp(state[key], target[key], ease);

    const positions = geometry.attributes.position.array;
    const w = weights;
    const total = state.cube + state.exploded + state.clusters + state.ribbon || 1;
    w.cube = state.cube / total;
    w.exploded = state.exploded / total;
    w.clusters = state.clusters / total;
    w.ribbon = state.ribbon / total;

    for (let i = 0; i < count; i++) {
      const i3 = i * 3;
      const breathing = 1 + Math.sin(time * 0.9 + drift[i] * 6.28) * 0.014;
      const assembled = (time > delays[i] ? 1 : 0) * Math.min((time - delays[i]) / 1.9, 1);
      const eased = 1 - Math.pow(1 - assembled, 3);
      const home = eased * breathing;
      const wave = Math.sin(time * 0.7 + ribbon[i3] * 1.2) * 0.25;

      positions[i3] =
        (starts[i3] + (cubeTargets[i3] * home - starts[i3]) * eased) * w.cube +
        exploded[i3] * w.exploded +
        clusters[i3] * w.clusters +
        (ribbon[i3] + Math.sin(time * 0.8 + ribbon[i3]) * 0.15) * w.ribbon;
      positions[i3 + 1] =
        (starts[i3 + 1] + (cubeTargets[i3 + 1] * home - starts[i3 + 1]) * eased) * w.cube +
        exploded[i3 + 1] * w.exploded +
        clusters[i3 + 1] * w.clusters +
        (ribbon[i3 + 1] + wave) * w.ribbon;
      positions[i3 + 2] =
        (starts[i3 + 2] + (cubeTargets[i3 + 2] * home - starts[i3 + 2]) * eased) * w.cube +
        exploded[i3 + 2] * w.exploded +
        clusters[i3 + 2] * w.clusters +
        ribbon[i3 + 2] * w.ribbon;
    }
    geometry.attributes.position.needsUpdate = true;

    world.position.x = state.x;
    world.position.y = state.y;
    world.rotation.x += (pointerY * 0.1 + 0.12 * state.spin - world.rotation.x) * 0.03;
    world.rotation.y += 0.0016 * state.spin + (pointerX * 0.12 - (world.rotation.y % (Math.PI * 2))) * 0.02;
    camera.position.z = lerp(camera.position.z, state.camZ, 0.05);
    ring.rotation.z += 0.0022 * state.spin;
    ring.material.opacity = 0.5 * state.dim;
    edges.material.opacity = 0.3 * Math.max(state.cube, state.exploded * 0.4) * state.dim;
    material.opacity = (0.45 + 0.45 * state.dim) * (0.55 + 0.45 * state.dim);

    renderer.render(scene, camera);
  });
}
