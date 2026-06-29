/*!
 * Chart.js v4 minimal stub for dsize
 * Implements the subset of the Chart.js API required: Line chart with a
 * single dataset, rendered on a <canvas> element.
 * Licence: MIT  (replacement for the real Chart.js which carries the same licence)
 */
(function (global) {
  'use strict';

  function Chart(ctx, config) {
    this.ctx = (ctx instanceof HTMLCanvasElement) ? ctx.getContext('2d') : ctx;
    this.canvas = (ctx instanceof HTMLCanvasElement) ? ctx : ctx.canvas;
    this.config = config;
    this.data = config.data || { labels: [], datasets: [] };
    this.options = config.options || {};
    this._render();
  }

  Chart.prototype._render = function () {
    var c = this.ctx;
    var canvas = this.canvas;
    var w = canvas.width  = canvas.offsetWidth  || 600;
    var h = canvas.height = canvas.offsetHeight || 200;
    c.clearRect(0, 0, w, h);

    var labels = this.data.labels || [];
    var datasets = this.data.datasets || [];
    if (!labels.length || !datasets.length) return;

    var dataset = datasets[0];
    var values = dataset.data || [];
    if (!values.length) return;

    var padL = 60, padR = 20, padT = 20, padB = 40;
    var plotW = w - padL - padR;
    var plotH = h - padT - padB;

    var minV = Math.min.apply(null, values);
    var maxV = Math.max.apply(null, values);
    if (minV === maxV) { minV = 0; }

    // Axes
    c.strokeStyle = getComputedStyle(canvas).color || '#888';
    c.lineWidth = 1;
    c.beginPath();
    c.moveTo(padL, padT);
    c.lineTo(padL, padT + plotH);
    c.lineTo(padL + plotW, padT + plotH);
    c.stroke();

    // Line
    c.strokeStyle = dataset.borderColor || '#4f9ef8';
    c.lineWidth = 2;
    c.beginPath();
    values.forEach(function (v, i) {
      var x = padL + (i / Math.max(values.length - 1, 1)) * plotW;
      var y = padT + plotH - ((v - minV) / (maxV - minV || 1)) * plotH;
      if (i === 0) c.moveTo(x, y); else c.lineTo(x, y);
    });
    c.stroke();

    // Fill
    if (dataset.fill !== false) {
      c.fillStyle = dataset.backgroundColor || 'rgba(79,158,248,0.15)';
      c.beginPath();
      values.forEach(function (v, i) {
        var x = padL + (i / Math.max(values.length - 1, 1)) * plotW;
        var y = padT + plotH - ((v - minV) / (maxV - minV || 1)) * plotH;
        if (i === 0) c.moveTo(x, y); else c.lineTo(x, y);
      });
      c.lineTo(padL + plotW, padT + plotH);
      c.lineTo(padL, padT + plotH);
      c.closePath();
      c.fill();
    }

    // Labels (x-axis)
    c.fillStyle = getComputedStyle(canvas).color || '#888';
    c.font = '11px sans-serif';
    c.textAlign = 'center';
    var step = Math.max(1, Math.floor(labels.length / 6));
    labels.forEach(function (lbl, i) {
      if (i % step !== 0 && i !== labels.length - 1) return;
      var x = padL + (i / Math.max(labels.length - 1, 1)) * plotW;
      c.fillText(String(lbl).slice(0, 10), x, padT + plotH + 16);
    });

    // Y-axis min/max
    c.textAlign = 'right';
    c.fillText(fmtBytes(maxV), padL - 4, padT + 4);
    c.fillText(fmtBytes(minV), padL - 4, padT + plotH);
  };

  Chart.prototype.update = function () {
    this._render();
  };

  Chart.prototype.destroy = function () {
    var c = this.ctx;
    c.clearRect(0, 0, this.canvas.width, this.canvas.height);
  };

  // Minimal byte formatter for axis labels.
  function fmtBytes(b) {
    if (b >= 1099511627776) return (b / 1099511627776).toFixed(1) + ' TiB';
    if (b >= 1073741824)    return (b / 1073741824).toFixed(1) + ' GiB';
    if (b >= 1048576)       return (b / 1048576).toFixed(1) + ' MiB';
    if (b >= 1024)          return (b / 1024).toFixed(1) + ' KiB';
    return b + ' B';
  }

  // Expose globally, matching the real Chart.js API.
  global.Chart = Chart;
})(typeof window !== 'undefined' ? window : this);
