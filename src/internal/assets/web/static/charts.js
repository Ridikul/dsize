/**
 * charts.js — wraps the bundled Chart.js for the dsize history line chart.
 * Depends on chart.js being loaded first.
 */
(function (global) {
  'use strict';

  var _chartInstance = null;

  /**
   * renderHistoryChart draws a "total size over time" line chart.
   * @param {HTMLCanvasElement} canvas
   * @param {Array<{scannedAt: string, totalSize: number}>} summaries - ascending by date
   */
  function renderHistoryChart(canvas, summaries) {
    if (_chartInstance) {
      _chartInstance.destroy();
      _chartInstance = null;
    }

    if (!summaries || summaries.length === 0) {
      var ctx2d = canvas.getContext('2d');
      ctx2d.clearRect(0, 0, canvas.width, canvas.height);
      return;
    }

    var labels = summaries.map(function (s) {
      return s.scannedAt ? s.scannedAt.slice(0, 10) : '';
    });
    var values = summaries.map(function (s) {
      return s.totalSize || 0;
    });

    _chartInstance = new Chart(canvas, {
      type: 'line',
      data: {
        labels: labels,
        datasets: [{
          label: 'Total Size',
          data: values,
          borderColor: '#4f9ef8',
          backgroundColor: 'rgba(79,158,248,0.12)',
          fill: true,
          tension: 0.3
        }]
      },
      options: {
        responsive: true,
        animation: false,
        plugins: {
          legend: { display: false }
        },
        scales: {
          y: { beginAtZero: true }
        }
      }
    });
  }

  global.dsizeCharts = { renderHistoryChart: renderHistoryChart };
})(window);
