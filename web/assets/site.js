const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;

const line = document.querySelector(".progress-line");
const trackProgress = () => {
  const denominator = Math.max(document.documentElement.scrollHeight - innerHeight, 1);
  line.style.width = `${(scrollY / denominator) * 100}%`;
};
trackProgress();
addEventListener("scroll", trackProgress, { passive: true });
addEventListener("resize", trackProgress);

const revealables = document.querySelectorAll("[data-reveal]");
if (reduced) {
  revealables.forEach((element) => element.classList.add("in"));
} else {
  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      entry.target.classList.add("in");
      observer.unobserve(entry.target);
    }
  }, { threshold: 0.12, rootMargin: "0px 0px -40px 0px" });
  revealables.forEach((element) => observer.observe(element));
}

const header = document.querySelector(".top");
const onScroll = () => header.classList.toggle("scrolled", scrollY > 12);
onScroll();
addEventListener("scroll", onScroll, { passive: true });

const spotlight = (element) => {
  element.addEventListener("pointermove", (event) => {
    const rect = element.getBoundingClientRect();
    element.style.setProperty("--mx", `${event.clientX - rect.left}px`);
    element.style.setProperty("--my", `${event.clientY - rect.top}px`);
  });
};

document.querySelectorAll(".cards article, .step").forEach(spotlight);

const frame = document.querySelector(".shot-frame");
if (frame && !reduced && matchMedia("(pointer: fine)").matches) {
  const shot = frame.querySelector("img");
  frame.addEventListener("pointermove", (event) => {
    const rect = frame.getBoundingClientRect();
    const x = (event.clientX - rect.left) / rect.width - 0.5;
    const y = (event.clientY - rect.top) / rect.height - 0.5;
    frame.style.transform = `perspective(1200px) rotateY(${x * 4}deg) rotateX(${-y * 3}deg)`;
    shot.style.boxShadow = `${-x * 26}px ${14 - y * 18}px 60px -30px rgba(0, 0, 0, 0.95)`;
  });
  frame.addEventListener("pointerleave", () => {
    frame.style.transform = "";
    shot.style.boxShadow = "";
  });
}

const counters = document.querySelectorAll("[data-count]");
if (counters.length) {
  const animate = (element) => {
    const target = Number(element.dataset.count);
    const started = performance.now();
    const duration = 1100;
    const tick = (now) => {
      const t = Math.min((now - started) / duration, 1);
      element.textContent = Math.round(target * (1 - Math.pow(1 - t, 3)));
      if (t < 1) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  };
  const counterObserver = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      counterObserver.unobserve(entry.target);
      if (reduced) {
        entry.target.textContent = entry.target.dataset.count;
      } else {
        animate(entry.target);
      }
    }
  }, { threshold: 0.5 });
  counters.forEach((element) => counterObserver.observe(element));
}

const terminal = document.querySelector("[data-type]");
if (terminal && !reduced) {
  const command = terminal.dataset.type;
  const observer = new IntersectionObserver((entries) => {
    if (!entries[0].isIntersecting) return;
    observer.disconnect();
    let position = 0;
    const tick = () => {
      position += 1;
      terminal.textContent = command.slice(0, position);
      if (position < command.length) {
        setTimeout(tick, 14 + Math.random() * 26);
      }
    };
    setTimeout(tick, 350);
  }, { threshold: 0.4 });
  observer.observe(terminal);
} else if (terminal) {
  terminal.textContent = terminal.dataset.type;
}
